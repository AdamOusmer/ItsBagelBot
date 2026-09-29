// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';
import { subscribe } from './raf-loop';

const SELECTOR = '[data-decode]';
const SCRAMBLE = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#$%&*+-/<>';
const BASE_DURATION_MS = 380;
const PER_CHAR_MS = 26;
const MAX_DURATION_MS = 1000;
const SCRAMBLE_ROLLS = 22;
const DEFAULT_THRESHOLD = 0.45;

export type ScrambleOptions = {
    charset?: string;
    durationMs?: number;
};

export type DecodeOptions = ScrambleOptions & {
    threshold?: number;
};

const lengthDuration = (length: number): number =>
    Math.min(MAX_DURATION_MS, BASE_DURATION_MS + length * PER_CHAR_MS);

const keepsShape = (char: string): boolean => char === ' ' || /\W/.test(char);

export function decode(el: HTMLElement, text: string, options: ScrambleOptions = {}): () => void {
    if (prefersReducedMotion()) {
        el.textContent = text;
        return () => {};
    }

    const chars = Array.from(text);
    const glyphs = Array.from(options.charset ?? SCRAMBLE);
    const duration = options.durationMs ?? lengthDuration(chars.length);
    const start = performance.now();

    return subscribe((now) => {
        const progress = Math.min(1, Math.max(0, (now - start) / duration));
        const revealCount = Math.floor(chars.length * progress);
        const roll = Math.floor(progress * SCRAMBLE_ROLLS);

        el.textContent = chars
            .map((char, i) => {
                if (keepsShape(char) || i < revealCount) return char;
                return glyphs[(i * 19 + roll * 7) % glyphs.length];
            })
            .join('');

        if (progress < 1) return;
        el.textContent = text;
        return false;
    });
}

export function observeDecode(root: ParentNode, options: DecodeOptions = {}): () => void {
    const { threshold = DEFAULT_THRESHOLD, ...scramble } = options;
    const self =
        root instanceof Element && root.matches(SELECTOR) ? [root as HTMLElement] : [];
    const targets = [...self, ...Array.from(root.querySelectorAll<HTMLElement>(SELECTOR))];
    const running = new Set<() => void>();

    const stopAll = (): void => {
        for (const stop of running) stop();
        running.clear();
    };

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
                const stop = decode(el, source.get(el) ?? '', scramble);
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
