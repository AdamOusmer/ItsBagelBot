// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { subscribe, wake } from './raf-loop';

const FALLBACK_REMEASURE_EVERY = 20;

type FrameQueue = {
    enqueue: () => void;
    cancel: () => void;
};

function readFraction(scrollY: number, total: number): number {
    return total > 0 ? Math.min(1, Math.max(0, scrollY / total)) : 0;
}

function createFrameQueue(run: () => void): FrameQueue {
    const frame = (): boolean => {
        run();
        return false;
    };

    return {
        enqueue: () => wake(frame),
        cancel: subscribe(frame),
    };
}

function timelineDrivesProgress(): boolean {
    return typeof CSS !== 'undefined' && CSS.supports?.('animation-timeline: scroll()') === true;
}

function entryHeight(entry: ResizeObserverEntry): number {
    return entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height;
}

// Resize must not read layout: iOS fires it on every step of the address bar animation.
function watchPageHeight(page: HTMLElement, onHeight: (height: number) => void): ResizeObserver | null {
    if (typeof ResizeObserver === 'undefined') return null;
    const observer = new ResizeObserver(([entry]) => onHeight(entryHeight(entry)));
    observer.observe(page);
    return observer;
}

export function mountReadingProgress(fill: HTMLElement): () => void {
    if (typeof window === 'undefined' || timelineDrivesProgress()) return () => {};

    const doc = document.documentElement;
    let docHeight = doc.scrollHeight;
    let total = 0;
    let updates = 0;
    let observer: ResizeObserver | null = null;

    const measure = (): void => {
        total = docHeight - window.innerHeight;
    };

    const remeasureUnobserved = (): void => {
        if (observer || updates++ % FALLBACK_REMEASURE_EVERY !== 0) return;
        docHeight = doc.scrollHeight;
        measure();
    };

    const paint = (): void => {
        remeasureUnobserved();
        fill.style.setProperty('--read', readFraction(window.scrollY, total).toFixed(4));
    };

    const queue = createFrameQueue(paint);

    const onResize = (): void => {
        measure();
        queue.enqueue();
    };

    window.addEventListener('scroll', queue.enqueue, { passive: true });
    window.addEventListener('resize', onResize, { passive: true });
    observer = watchPageHeight(doc, (height) => {
        docHeight = height;
        onResize();
    });
    measure();
    paint();

    return () => {
        window.removeEventListener('scroll', queue.enqueue);
        window.removeEventListener('resize', onResize);
        observer?.disconnect();
        queue.cancel();
    };
}
