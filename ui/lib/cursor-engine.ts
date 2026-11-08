// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { CURSOR_PROP } from './cursor-prop';
import { mediaQuery, reduceMotion } from './motion-query';
import { subscribe, wake } from './raf-loop';

export type CursorEase = {
    hover: number;
    release: number;
};

export type CursorOptions = {
    dot: HTMLElement;
    ring: HTMLElement;
    selector?: string;
    ease?: CursorEase;
};

const SELECTOR = 'a, button, .bb-input';
const TEXT =
    '.bb-input:not(.bb-input--select), textarea, [contenteditable]:not([contenteditable="false"]), ' +
    'input:not([type="checkbox"], [type="radio"], [type="range"], [type="color"], [type="file"], ' +
    '[type="button"], [type="submit"], [type="reset"], [type="image"], [type="hidden"])';
const EASE: CursorEase = { hover: 0.3, release: 0.28 };

const IDLE_SIZE = 36;
const HOVER_PAD = 6;
const FALLBACK_RADIUS = 8;
const SETTLE_PX = 0.3;
const FRAME_MS = 1000 / 60;
const cursorPointer = mediaQuery('(min-width: 901px) and (hover: hover) and (pointer: fine)');

let pointerX = -1;
let pointerY = -1;

type Box = { x: number; y: number; w: number; h: number; r: number };

const lerp = (a: number, b: number, t: number): number => a + (b - a) * t;

function idleBox(x: number, y: number): Box {
    const half = IDLE_SIZE / 2;
    return { x: x - half, y: y - half, w: IDLE_SIZE, h: IDLE_SIZE, r: half };
}

function radiusOf(style: CSSStyleDeclaration): number {
    return parseFloat(style.borderRadius) || FALLBACK_RADIUS;
}

function hoverBox(el: HTMLElement, radius: number): Box {
    const bounds = el.getBoundingClientRect();
    const h = bounds.height + HOVER_PAD * 2;
    return {
        x: bounds.left - HOVER_PAD,
        y: bounds.top - HOVER_PAD,
        w: bounds.width + HOVER_PAD * 2,
        h,
        r: Math.min(radius + HOVER_PAD, h / 2),
    };
}

function lerpBox(from: Box, to: Box, e: number): Box {
    return {
        x: lerp(from.x, to.x, e),
        y: lerp(from.y, to.y, e),
        w: lerp(from.w, to.w, e),
        h: lerp(from.h, to.h, e),
        r: to.r,
    };
}

function paintRing(ring: HTMLElement, box: Box, shape: Box, hovering: boolean, text: boolean): void {
    // Lay out the destination shape once; animate its position and scale only.
    ring.style.transform = `translate(${box.x.toFixed(1)}px, ${box.y.toFixed(1)}px) scale(${(box.w / shape.w).toFixed(4)}, ${(box.h / shape.h).toFixed(4)})`;
    const width = `${Number(shape.w.toFixed(1))}px`;
    const height = `${Number(shape.h.toFixed(1))}px`;
    const radius = `${Number(shape.r.toFixed(1))}px`;
    if (ring.style.width !== width) ring.style.width = width;
    if (ring.style.height !== height) ring.style.height = height;
    if (ring.style.borderRadius !== radius) ring.style.borderRadius = radius;
    ring.classList.toggle('is-morphed', hovering);
    ring.classList.toggle('is-text', text);
}

function arrived(box: Box, to: Box): boolean {
    return (
        Math.abs(box.x - to.x) < SETTLE_PX &&
        Math.abs(box.y - to.y) < SETTLE_PX &&
        Math.abs(box.w - to.w) < SETTLE_PX &&
        Math.abs(box.h - to.h) < SETTLE_PX
    );
}

export function mountCursor(options: CursorOptions): () => void {
    const { dot, ring, selector = SELECTOR, ease = EASE } = options;

    if (pointerX < 0) {
        pointerX = window.innerWidth / 2;
        pointerY = window.innerHeight / 2;
    }

    let box = idleBox(pointerX, pointerY);
    let target: HTMLElement | null = null;
    let overText = false;
    let lastFrame = 0;
    let active = false;
    let radius = FALLBACK_RADIUS;

    function setTarget(next: HTMLElement | null, style?: CSSStyleDeclaration): void {
        target = next;
        if (next && style) radius = radiusOf(style);
    }

    function hovered(): HTMLElement | null {
        return target?.isConnected ? target : null;
    }

    function paint(el: HTMLElement | null, shape: Box): void {
        dot.style.transform = `translate(${pointerX}px, ${pointerY}px)`;
        dot.style.opacity = el && !overText ? '0' : '1';
        dot.classList.toggle('is-text', overText);

        paintRing(ring, box, shape, !!el, overText);
    }

    function tick(now: number): boolean | void {
        if (!active) return false;
        const el = hovered();
        // Read geometry before any cursor writes, avoiding a forced layout.
        const to = el ? hoverBox(el, radius) : idleBox(pointerX, pointerY);
        const elapsed = lastFrame ? now - lastFrame : FRAME_MS;
        const factor = 1 - (1 - (el ? ease.hover : ease.release)) ** (elapsed / FRAME_MS);
        box = lerpBox(box, to, factor);
        const done = arrived(box, to);
        if (done) box = to;
        lastFrame = done ? 0 : now;
        paint(el, to);
        if (done) return false;
    }

    const nearest = (event: PointerEvent): HTMLElement | null => {
        for (let el = event.target as Element | null; el; el = el.parentElement) {
            if (el.matches(selector) || getComputedStyle(el).getPropertyValue(CURSOR_PROP) !== '') return el as HTMLElement;
        }
        return null;
    };

    const onMove = (event: PointerEvent): void => {
        if (!active || event.pointerType === 'touch') return;
        pointerX = event.clientX;
        pointerY = event.clientY;
        wake(tick);
    };

    const onOver = (event: PointerEvent): void => {
        if (!active || event.pointerType === 'touch') return;
        const hit = (event.target as Element | null)?.closest(`${selector}, ${TEXT}`);
        const text = !!hit?.matches(TEXT);
        if (text !== overText) {
            overText = text;
            wake(tick);
        }
        const el = nearest(event);
        if (!el) return;
        const style = getComputedStyle(el);
        const quiet = style.getPropertyValue(CURSOR_PROP).trim() === 'quiet';
        setTarget(quiet ? null : el, style);
        wake(tick);
    };

    const onOut = (event: PointerEvent): void => {
        if (!active || !target || nearest(event) !== target) return;
        setTarget(null);
        wake(tick);
    };

    const remeasure = (): void => {
        if (active && target) wake(tick);
    };

    const layered = [ring, dot].filter((el) => typeof el.showPopover === 'function');

    const raise = (): void => {
        for (const el of layered) {
            if (el.matches(':popover-open')) el.hidePopover();
            el.showPopover();
        }
    };

    const onToggle = (event: Event): void => {
        if ((event as ToggleEvent).newState !== 'open') return;
        if (layered.includes(event.target as HTMLElement)) return;
        queueMicrotask(raise);
    };

    const syncMotion = (): void => {
        active = cursorPointer.matches && !reduceMotion.matches;
        lastFrame = 0;
        document.documentElement.classList.toggle('bb-cursor-on', active);
        if (active) wake(tick);
    };

    const unsubscribe = subscribe(tick);
    for (const el of layered) el.popover = 'manual';
    raise();
    syncMotion();
    reduceMotion.addEventListener('change', syncMotion);
    cursorPointer.addEventListener('change', syncMotion);
    window.addEventListener('pointermove', onMove, { passive: true });
    window.addEventListener('scroll', remeasure, { passive: true, capture: true });
    window.addEventListener('resize', remeasure, { passive: true });
    document.addEventListener('pointerover', onOver, { passive: true });
    document.addEventListener('pointerout', onOut, { passive: true });
    document.addEventListener('beforetoggle', onToggle, true);

    return () => {
        unsubscribe();
        reduceMotion.removeEventListener('change', syncMotion);
        cursorPointer.removeEventListener('change', syncMotion);
        window.removeEventListener('pointermove', onMove);
        window.removeEventListener('scroll', remeasure, true);
        window.removeEventListener('resize', remeasure);
        document.removeEventListener('pointerover', onOver);
        document.removeEventListener('pointerout', onOut);
        document.removeEventListener('beforetoggle', onToggle, true);
        for (const el of layered) el.removeAttribute('popover');
        document.documentElement.classList.remove('bb-cursor-on');
    };
}
