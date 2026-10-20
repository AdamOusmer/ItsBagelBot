// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import Lenis, { type LenisOptions } from 'lenis';

import { prefersReducedMotion } from './motion-query';
import { createNestedScrollGate } from './nested-scroll';
import { subscribe, wake, type Tick } from './raf-loop';

const LERP = 0.1;

const SHARED_OPTIONS = { lerp: LERP, smoothWheel: true, syncTouch: false } satisfies LenisOptions;

type LenisWindow = Window & { __lenis?: Lenis };

export type SmoothScrollOptions = Pick<LenisOptions, 'prevent' | 'virtualScroll'>;

export type SmoothScroll = {
    lenis: Lenis;
    destroy: () => void;
};

const FRAME_MS = 1000 / 60;
const MAX_FRAME_MS = 100;

// Lenis integrates by elapsed time, so a wake after a sleep must not feed it the whole idle gap.
// The smallest consecutive interval seen is the refresh interval; dropped frames never widen it.
function driveLenis(lenis: Lenis): () => void {
    let last = 0;
    let frameMs = FRAME_MS;
    let measured = false;

    const tick: Tick = (now) => {
        if (last === 0) lenis.time = now - frameMs;
        else if (now - last < MAX_FRAME_MS) {
            frameMs = measured ? Math.min(frameMs, now - last) : now - last;
            measured = true;
        }
        last = now;
        lenis.raf(now);
        if (lenis.isScrolling === 'smooth') return;
        last = 0;
        return false;
    };
    const wakeTick = (): void => wake(tick);
    const scrollTo = lenis.scrollTo.bind(lenis);

    lenis.scrollTo = (...args: Parameters<Lenis['scrollTo']>): void => {
        scrollTo(...args);
        wakeTick();
    };
    lenis.on('virtual-scroll', wakeTick);
    const unsubscribe = subscribe(tick);

    return () => {
        unsubscribe();
        lenis.off('virtual-scroll', wakeTick);
    };
}

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

    const unsubscribe = driveLenis(lenis);

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
    const unsubscribe = driveLenis(lenis);

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
