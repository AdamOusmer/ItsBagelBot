// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The custom cursor: a tan dot tracking the pointer 1:1 and a ring that lerps
 * behind it and MORPHS into the box of whatever interactive element the
 * pointer is over.
 *
 * This existed twice, line for line: web/marketing/src/components/ui/
 * Cursor.astro and web/kit/components/Cursor.svelte. Not "similar" — the same
 * physics, the same magic numbers, the same settle timer, typed out twice. The
 * two had already drifted in the way two copies always drift: on the numbers
 * that were tuned AFTER the copy was made. The marketing pair was retuned on
 * 2026-08-31 (record below); the console never got it and was still running
 * hover 0.25 / release 0.16 the day this file was written, which is why the
 * console cursor read as sluggish next to the site and nobody could say why.
 *
 * Framework-free by construction: it takes two elements and returns a
 * teardown. Astro drives it from a DOM scan with ClientRouter teardown, Svelte
 * drives it from an `$effect` gated on the user's preference store, and neither
 * lifecycle leaks into the other.
 *
 * EASE PAIR — hover 0.3, release 0.28 (2026-08-31, carried over verbatim from
 * Cursor.astro:126-131):
 *   Release ease was raised 0.16 -> 0.28. User feedback read the trailing ring
 *   as site-wide sluggishness: at 0.16 the ring sat a full ring-width behind
 *   the dot on an ordinary sweep, and because the ring is the only element
 *   that lags, the whole page felt like it was lagging. 0.28 keeps a hint of
 *   trail; past ~0.35 the ring hard-locks to the dot and the two layers read as
 *   one blob, which loses the effect entirely. Hover stays marginally faster
 *   (0.3) so the morph onto a button snaps and the release drifts — asymmetry
 *   is the point, matched values read as elastic.
 *
 * OPT-OUT ATTRIBUTE — `data-cursor="quiet"`:
 *   There were two spellings of one idea. Marketing used `data-cursor="quiet"`
 *   (a whole card is a link, but stamping the ring onto a 700px row read as a
 *   flash, so the row stays idle and only a nested `data-cursor` element — the
 *   card's own button — takes the morph). The console used
 *   `data-cursor="off"` for exactly the same reason on list rows. One engine
 *   cannot honour two names without becoming a lookup table of synonyms, so
 *   "quiet" wins: it says what happens (the ring goes quiet) rather than
 *   implying the cursor is disabled there, and it was the name attached to the
 *   comment that explained the reasoning. The seven console call sites were
 *   renamed in the same commit (ManagementRow, Modal, ShardRow, the admin
 *   event feed, LoyaltyGameRow and ModuleIndexRow twice).
 *
 *   The two also differed in HOW they matched, and that difference is real:
 *   marketing asked whether the NEAREST element matching the selector is quiet,
 *   the console asked whether ANY ancestor is. The nearest-element rule is kept
 *   because it is the one that makes nesting work — a quiet row containing a
 *   real button lets the button morph. Under the ancestor rule it could not,
 *   which is why the console rows had to mark their own primary button quiet
 *   as well (see ManagementRow). Net effect of the change: a link inside a
 *   quiet row now morphs where it previously did not. That is the behaviour
 *   marketing has shipped since August and the reason the attribute exists.
 */

import { finePointer, reduceMotion } from './motion-query';
import { subscribe, wake } from './raf-loop';

/** Lerp factors, 0..1 per frame. Higher is faster; see the record above. */
export type CursorEase = {
    /** Ring chasing the box of the element under the pointer. */
    hover: number;
    /** Ring falling back to its idle circle around the dot. */
    release: number;
};

export type CursorOptions = {
    /** The 1:1 dot. */
    dot: HTMLElement;
    /** The lerping, morphing ring. */
    ring: HTMLElement;
    /** What counts as interactive. Union of both surfaces' lists. */
    selector?: string;
    /**
     * Selector (not a bare attribute name, despite the option name both
     * surfaces already used) for "matched, but keep the ring idle here".
     */
    quietAttr?: string;
    ease?: CursorEase;
};

/**
 * `a, button` are the marketing list; `.search` is the console's search field,
 * which is a div with an inner input and so matches neither. `[data-cursor]` is
 * the opt-in for anything else, and is also what makes a nested element inside
 * a quiet region eligible again.
 */
const SELECTOR = 'a, button, .search, [data-cursor]';
const QUIET = '[data-cursor="quiet"]';
const EASE: CursorEase = { hover: 0.3, release: 0.28 };

/** Idle ring: a 36px circle centred on the dot. */
const IDLE_SIZE = 36;
/** Breathing room between the morphed ring and the element it stamps onto. */
const HOVER_PAD = 6;
/** Fallback border radius for an element that reports none. */
const FALLBACK_RADIUS = 8;
/** Sub-pixel distance at which the ring counts as arrived. */
const SETTLE_PX = 0.3;
/** Quiet time after the last pointer move before the loop is allowed to stop. */
const IDLE_MS = 500;

/**
 * Last pointer position, module-level rather than per-mount.
 *
 * The Astro adapter disposes on `astro:before-swap` and mounts again on
 * `astro:page-load`, so a navigation destroys and recreates the cursor. Per-
 * mount state would restart it at the centre of the viewport, and since the
 * loop only runs while the pointer is moving, the ring would sit stranded in
 * the middle of the page until the visitor moved the mouse. The previous
 * marketing implementation avoided that with `transition:persist` on both
 * elements; that is gone because it makes Astro emit a
 * `data-astro-transition-persist` attribute the Svelte adapter cannot emit,
 * and adapter parity (ui/test/parity.test.ts) is the property this package is
 * built on. Remembering two numbers costs 16 bytes and restores the behaviour
 * exactly. Only one cursor exists per page, so there is nothing to collide
 * with.
 */
let pointerX = -1;
let pointerY = -1;

/** The ring's geometry in px: top-left corner, size, corner radius. */
type Box = { x: number; y: number; w: number; h: number; r: number };

const lerp = (a: number, b: number, t: number): number => a + (b - a) * t;

function idleBox(x: number, y: number): Box {
    const half = IDLE_SIZE / 2;
    return { x: x - half, y: y - half, w: IDLE_SIZE, h: IDLE_SIZE, r: half };
}

function hoverBox(el: HTMLElement): Box {
    const bounds = el.getBoundingClientRect();
    const h = bounds.height + HOVER_PAD * 2;
    const radius = parseFloat(getComputedStyle(el).borderRadius) || FALLBACK_RADIUS;
    return {
        x: bounds.left - HOVER_PAD,
        y: bounds.top - HOVER_PAD,
        w: bounds.width + HOVER_PAD * 2,
        h,
        // Inherit the element's own corner radius plus the pad, clamped to a
        // pill: a tall element with a huge radius would otherwise draw a lens.
        r: Math.min(radius + HOVER_PAD, h / 2),
    };
}

function lerpBox(from: Box, to: Box, e: number): Box {
    return {
        x: lerp(from.x, to.x, e),
        y: lerp(from.y, to.y, e),
        w: lerp(from.w, to.w, e),
        h: lerp(from.h, to.h, e),
        r: lerp(from.r, to.r, e),
    };
}

function paintRing(ring: HTMLElement, box: Box, hovering: boolean): void {
    ring.style.transform = `translate(${box.x.toFixed(1)}px, ${box.y.toFixed(1)}px)`;
    ring.style.width = `${box.w.toFixed(1)}px`;
    ring.style.height = `${box.h.toFixed(1)}px`;
    ring.style.borderRadius = `${box.r.toFixed(1)}px`;
    ring.classList.toggle('is-morphed', hovering);
}

function arrived(box: Box, to: Box): boolean {
    return Math.abs(box.x - to.x) < SETTLE_PX && Math.abs(box.y - to.y) < SETTLE_PX;
}

/**
 * Wire `dot` and `ring` to the pointer. Returns the teardown.
 *
 * Returns a no-op teardown, having done nothing at all, when there is no fine
 * pointer: a touch device gets no `bb-cursor-on` class, no listeners and no
 * frames. That check is deliberately NOT re-evaluated live, unlike reduced
 * motion — a visitor does not grow a mouse mid-session, and the CSS hides both
 * elements on coarse pointers anyway.
 */
export function mountCursor(options: CursorOptions): () => void {
    const { dot, ring, selector = SELECTOR, quietAttr = QUIET, ease = EASE } = options;
    if (!finePointer.matches) return () => {};

    if (pointerX < 0) {
        pointerX = window.innerWidth / 2;
        pointerY = window.innerHeight / 2;
    }

    let box = idleBox(pointerX, pointerY);
    let target: HTMLElement | null = null;
    let lastMove = 0;

    /**
     * The element the ring is stamped onto this frame, or null for idle.
     *
     * The `isConnected` check is not paranoia: `pointerout` does not fire for
     * an element that is removed from the document while the pointer is over
     * it (a menu closing under the cursor, a row that finishes saving), so
     * without this the ring would stay morphed around a box that no longer
     * exists, lerping towards a rect of zeros.
     */
    function hovered(): HTMLElement | null {
        return target?.isConnected ? target : null;
    }

    /** Draw one frame. Returns the box the ring is heading for. */
    function paint(el: HTMLElement | null): Box {
        dot.style.transform = `translate(${pointerX}px, ${pointerY}px)`;
        // The dot fades while the ring is stamped onto an element: two tan
        // marks on one button reads as a rendering bug, not as a cursor.
        dot.style.opacity = el ? '0' : '1';

        const to = el ? hoverBox(el) : idleBox(pointerX, pointerY);
        box = lerpBox(box, to, el ? ease.hover : ease.release);
        paintRing(ring, box, !!el);
        return to;
    }

    // Returning false unsubscribes from the shared rAF loop until the next
    // wake(). Reduced motion parks the cursor where it stands rather than
    // freezing mid-lerp, which is why the class toggle happens separately —
    // and it parks even while hovering, because the frame that has just been
    // painted is a legitimate resting place and the next pointer event will
    // wake the loop again.
    function tick(now: number): boolean | void {
        const el = hovered();
        const to = paint(el);
        if (reduceMotion.matches) return false;
        if (el) return;
        if (arrived(box, to) && now - lastMove > IDLE_MS) return false;
    }

    const nearest = (event: PointerEvent): HTMLElement | null =>
        (event.target as Element | null)?.closest<HTMLElement>(selector) ?? null;

    const onMove = (event: PointerEvent): void => {
        // A touch that lands on a hybrid device would otherwise teleport the
        // dot to the tap and leave it there.
        if (event.pointerType === 'touch') return;
        pointerX = event.clientX;
        pointerY = event.clientY;
        lastMove = performance.now();
        wake(tick);
    };

    // Delegated, so elements added after mount (a modal, a swapped page) are
    // covered without rescanning anything.
    const onOver = (event: PointerEvent): void => {
        const el = nearest(event);
        if (!el) return;
        target = el.matches(quietAttr) ? null : el;
        wake(tick);
    };

    const onOut = (event: PointerEvent): void => {
        if (!target || nearest(event) !== target) return;
        target = null;
        wake(tick);
    };

    // The native pointer is hidden by CSS keyed on this class, set from script
    // so it is never hidden before the cursor that replaces it exists. Reduced
    // motion gives the native pointer back live, without a reload.
    const syncMotion = (): void => {
        document.documentElement.classList.toggle('bb-cursor-on', !reduceMotion.matches);
        wake(tick);
    };

    const unsubscribe = subscribe(tick);
    syncMotion();
    reduceMotion.addEventListener('change', syncMotion);
    window.addEventListener('pointermove', onMove, { passive: true });
    document.addEventListener('pointerover', onOver, { passive: true });
    document.addEventListener('pointerout', onOut, { passive: true });

    return () => {
        unsubscribe();
        reduceMotion.removeEventListener('change', syncMotion);
        window.removeEventListener('pointermove', onMove);
        document.removeEventListener('pointerover', onOver);
        document.removeEventListener('pointerout', onOut);
        document.documentElement.classList.remove('bb-cursor-on');
    };
}
