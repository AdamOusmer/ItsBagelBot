// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type MotionQuery = {
    readonly matches: boolean;
    addEventListener(type: 'change', listener: () => void): void;
    removeEventListener(type: 'change', listener: () => void): void;
};

const ABSENT: MotionQuery = {
    matches: false,
    addEventListener() {},
    removeEventListener() {},
};

function query(feature: string): MotionQuery {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return ABSENT;
    return window.matchMedia(feature);
}

export function mediaQuery(feature: string): MotionQuery {
    return query(feature);
}

export const reduceMotion: MotionQuery = query('(prefers-reduced-motion: reduce)');

export const finePointer: MotionQuery = query('(hover: hover) and (pointer: fine)');

export function prefersReducedMotion(): boolean {
    return reduceMotion.matches;
}

export function hasFinePointer(): boolean {
    return finePointer.matches;
}
