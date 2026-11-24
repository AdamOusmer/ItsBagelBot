// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';
import { subscribe } from './raf-loop';

const SCRAMBLE = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#$%&*+-/<>';
const BASE_DURATION_MS = 380;
const PER_CHAR_MS = 26;
const MAX_DURATION_MS = 1000;
const SCRAMBLE_ROLLS = 22;
const DEFAULT_THRESHOLD = 0.45;
const SHOWN_OPACITY = 0.05;

export type ScrambleOptions = {
    charset?: string;
    durationMs?: number;
};

export type DecodeOptions = ScrambleOptions & {
    threshold?: number;
    instant?: boolean;
};

const lengthDuration = (length: number): number =>
    Math.min(MAX_DURATION_MS, BASE_DURATION_MS + length * PER_CHAR_MS);

const keepsShape = (char: string): boolean => char === ' ' || /\W/.test(char);

const scrambleFrame = (chars: string[], glyphs: string[], progress: number): string => {
    const revealCount = Math.floor(chars.length * progress);
    const roll = Math.floor(progress * SCRAMBLE_ROLLS);
    return chars
        .map((char, i) => {
            if (keepsShape(char) || i < revealCount) return char;
            return glyphs[(i * 19 + roll * 7) % glyphs.length];
        })
        .join('');
};

const glyphsOf = (options: ScrambleOptions): string[] => Array.from(options.charset ?? SCRAMBLE);

const shownOpacity = (el: Element): number => {
    let opacity = 1;
    for (let node: Element | null = el; node; node = node.parentElement) opacity *= parseFloat(getComputedStyle(node).opacity);
    return opacity;
};

function whenShown(el: Element, run: () => () => void): () => void {
    if (shownOpacity(el) > SHOWN_OPACITY) return run();
    let stopRun = (): void => {};
    const stopWatch = subscribe(() => {
        if (shownOpacity(el) <= SHOWN_OPACITY) return;
        stopWatch();
        stopRun = run();
        return false;
    });
    return () => {
        stopWatch();
        stopRun();
    };
}

export function decode(
    el: HTMLElement,
    text: string,
    options: ScrambleOptions = {},
    onDone?: () => void,
): () => void {
    if (prefersReducedMotion()) {
        el.textContent = text;
        return () => {};
    }

    const chars = Array.from(text);
    const glyphs = glyphsOf(options);
    const duration = options.durationMs ?? lengthDuration(chars.length);
    const start = performance.now();

    const stop = subscribe((now) => {
        const progress = Math.min(1, Math.max(0, (now - start) / duration));
        el.textContent = scrambleFrame(chars, glyphs, progress);

        if (progress < 1) return;
        el.textContent = text;
        stop();
        onDone?.();
        return false;
    });
    return stop;
}

const decodesInstantly = (instant: boolean | undefined): boolean =>
    !!instant || typeof IntersectionObserver === 'undefined' || prefersReducedMotion();

function scrambleAll(source: Map<HTMLElement, string>, scramble: ScrambleOptions): void {
    const glyphs = glyphsOf(scramble);
    for (const [el, text] of source) el.textContent = scrambleFrame(Array.from(text), glyphs, 0);
}

export function observeDecode(elements: Iterable<HTMLElement>, options: DecodeOptions = {}): () => void {
    const { threshold = DEFAULT_THRESHOLD, instant, ...scramble } = options;
    const source = new Map(Array.from(elements, (el) => [el, el.textContent ?? ''] as const));
    const running = new Set<() => void>();

    const stopAll = (): void => {
        for (const stop of running) stop();
        running.clear();
    };

    if (decodesInstantly(instant)) {
        for (const [el, text] of source) el.textContent = text;
        return stopAll;
    }

    scrambleAll(source, scramble);

    const start = (el: HTMLElement): void => {
        const stop = whenShown(el, () => decode(el, source.get(el) ?? '', scramble, () => running.delete(stop)));
        running.add(stop);
    };

    const observer = new IntersectionObserver(
        (entries) => {
            for (const entry of entries.filter((e) => e.isIntersecting)) {
                observer.unobserve(entry.target);
                start(entry.target as HTMLElement);
            }
        },
        { threshold },
    );

    for (const el of source.keys()) observer.observe(el);

    return () => {
        observer.disconnect();
        stopAll();
    };
}
