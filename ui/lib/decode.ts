// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Decode-on-view text: the brand's "decrypt" reveal.
 *
 * An element tagged `data-decode` scrambles the first time it scrolls into
 * view and then resolves character by character, left to right. It came out of
 * the homepage's encryption scene and is now the page hero's title treatment
 * and the mono receipt lines under the steps.
 *
 * From web/marketing/src/script/decode.js. Two things changed on the way in,
 * and both are the reason this is a library module rather than a copied file:
 *
 *  1. It exports an `observeDecode(root) -> dispose` the way ui/lib/reveal.ts
 *     does, instead of wiring `astro:page-load` itself at import time. The
 *     package declares `sideEffects: ["**\/*.css"]`, so a JS module whose only
 *     job happens on import is one a bundler is entitled to drop; the site's
 *     binder calls this instead.
 *  2. Frames come from ui/lib/raf-loop's single scheduler rather than each
 *     running element owning a `requestAnimationFrame` chain. Six of those
 *     chains on one page was the thing the shared loop exists to delete, and
 *     the loop already suspends itself on a hidden tab, which the old chains
 *     did not.
 *
 * THE SCRAMBLE ALPHABET is uppercase latin, digits and eight symbols. No
 * lowercase, deliberately: mixed-case noise has visibly different glyph
 * heights, so the line's baseline texture flickers as it resolves and reads as
 * a rendering fault rather than an effect.
 *
 * PUNCTUATION AND SPACES ARE NEVER SCRAMBLED (`/\W/`). Word shape is what lets
 * a reader recognise the text a beat before it finishes resolving, and that
 * recognition is the whole payoff. Scrambling the spaces turns it into a
 * uniform block of noise that resolves into a surprise.
 *
 * DURATION `min(1000, 380 + 26 per character)`. It scales with length so a
 * three-word title and a one-line receipt both feel like the same effect, and
 * it is capped at one second because past that the visitor has started reading
 * and the resolve is competing with them. `t = floor(progress * 22)` is the
 * noise's own clock: the scramble re-rolls 22 times over the run regardless of
 * frame rate, so a 120Hz display does not get a faster-churning effect than a
 * 60Hz one.
 *
 * REDUCED MOTION sets the final text and returns. Not a shorter scramble: text
 * that rewrites itself is exactly the thing the preference is asking not to
 * see.
 */

import { prefersReducedMotion } from './motion-query';
import { subscribe } from './raf-loop';

const SELECTOR = '[data-decode]';
const SCRAMBLE = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#$%&*+-/<>';

export type DecodeOptions = {
    /**
     * Default `0.45`. High compared with the reveal observer's 0.08, and for
     * the opposite reason: a decode is a short, self-contained effect on a
     * single line of text, so it should start when the line is properly on
     * screen rather than as its first pixel crosses. A decoding title half cut
     * off by the fold is illegible noise.
     */
    threshold?: number;
};

const DEFAULTS: Required<DecodeOptions> = { threshold: 0.45 };

/** Run the scramble on one element, resolving to `text`. Returns its stopper. */
function runDecode(el: HTMLElement, text: string): () => void {
    if (prefersReducedMotion()) {
        el.textContent = text;
        return () => {};
    }

    const chars = Array.from(text);
    const duration = Math.min(1000, 380 + chars.length * 26);
    const start = performance.now();

    return subscribe((now) => {
        const progress = Math.min(1, (now - start) / duration);
        const revealCount = Math.floor(chars.length * progress);
        const t = Math.floor(progress * 22);

        el.textContent = chars
            .map((char, i) => {
                if (char === ' ' || /\W/.test(char)) return char;
                if (i < revealCount) return char;
                return SCRAMBLE[(i * 19 + t * 7) % SCRAMBLE.length];
            })
            .join('');

        if (progress < 1) return;
        el.textContent = text;
        return false;
    });
}

/**
 * Wire every `[data-decode]` under `root`. Returns a dispose that disconnects
 * the observer and stops any scramble still running.
 *
 * Disposing MATTERS here in a way it does not for a pure observer: a swap
 * mid-scramble leaves a tick writing `textContent` into a detached node
 * forever, because the element it holds never intersects anything again and
 * the run only ends on its own clock. The old script tracked frame ids in a
 * module-level Set for exactly this and cancelled them on `astro:before-swap`.
 */
export function observeDecode(root: ParentNode, options: DecodeOptions = {}): () => void {
    const { threshold } = { ...DEFAULTS, ...options };
    // The root counts if it carries the attribute, so `use:decode` on the
    // title element and on its wrapper both do the obvious thing. Same rule as
    // ./reveal.ts's `targetsIn`, and the same trap if it is left out: an action
    // placed directly on the one element that wants the effect silently does
    // nothing.
    const self =
        root instanceof Element && root.matches(SELECTOR) ? [root as HTMLElement] : [];
    const targets = [...self, ...Array.from(root.querySelectorAll<HTMLElement>(SELECTOR))];
    const running = new Set<() => void>();

    const stopAll = (): void => {
        for (const stop of running) stop();
        running.clear();
    };

    // The source text is the `data-decode` attribute when it carries one, and
    // the element's own text otherwise. Read BEFORE the first frame overwrites
    // it, which is why it is captured here rather than inside the tick.
    const source = new Map(
        targets.map((el) => [el, el.dataset.decode?.length ? el.dataset.decode : (el.textContent ?? '')]),
    );

    if (typeof IntersectionObserver === 'undefined') {
        for (const el of targets) el.textContent = source.get(el) ?? '';
        return stopAll;
    }

    const observer = new IntersectionObserver(
        (entries) => {
            for (const entry of entries) {
                if (!entry.isIntersecting) continue;
                const el = entry.target as HTMLElement;
                observer.unobserve(el);
                const stop = runDecode(el, source.get(el) ?? '');
                running.add(stop);
            }
        },
        { threshold },
    );

    for (const el of targets) observer.observe(el);

    return () => {
        observer.disconnect();
        stopAll();
    };
}
