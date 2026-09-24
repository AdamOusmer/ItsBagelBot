// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import Lenis, { type LenisOptions } from 'lenis';

import { prefersReducedMotion } from './motion-query';
import { createNestedScrollGate } from './nested-scroll';
import { subscribe } from './raf-loop';

const LERP = 0.1;

const SHARED_OPTIONS = { lerp: LERP, smoothWheel: true, syncTouch: false } satisfies LenisOptions;

type LenisWindow = Window & { __lenis?: Lenis };

export type SmoothScrollOptions = Pick<LenisOptions, 'prevent' | 'virtualScroll'>;

export type SmoothScroll = {
    lenis: Lenis;
    destroy: () => void;
};

export function createSmoothScroll(options: SmoothScrollOptions = {}): SmoothScroll | null {
    if (typeof window === 'undefined') return null;

    const existing = (window as LenisWindow).__lenis;
    if (existing) return { lenis: existing, destroy: () => {} };

    if (prefersReducedMotion()) return null;

    const gate = createNestedScrollGate(options);
    const lenis = new Lenis({
        ...SHARED_OPTIONS,
        ...options,
        prevent: gate.prevent,
        virtualScroll: gate.virtualScroll,
    });

    (window as LenisWindow).__lenis = lenis;

    const unsubscribe = subscribe((now) => {
        lenis.raf(now);
    });

    return {
        lenis,
        destroy() {
            unsubscribe();
            if ((window as LenisWindow).__lenis === lenis) delete (window as LenisWindow).__lenis;
            lenis.destroy();
        },
    };
}

export function createPaneScroll(wrapper: HTMLElement): SmoothScroll | null {
    if (typeof window === 'undefined' || prefersReducedMotion()) return null;

    const gate = createNestedScrollGate({ virtualScroll: () => lenis.limit > 0 });
    const lenis: Lenis = new Lenis({
        ...SHARED_OPTIONS,
        wrapper,
        content: wrapper,
        autoResize: false,
        naiveDimensions: true,
        prevent: gate.prevent,
        virtualScroll: gate.virtualScroll,
    });
    const unsubscribe = subscribe((now) => {
        lenis.raf(now);
    });

    return {
        lenis,
        destroy() {
            unsubscribe();
            lenis.destroy();
        },
    };
}

export function getSmoothScroll(): Lenis | undefined {
    if (typeof window === 'undefined') return undefined;
    return (window as LenisWindow).__lenis;
}
