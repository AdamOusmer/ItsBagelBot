// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { subscribe } from './raf-loop';

const FALLBACK_REMEASURE_EVERY = 20;

type FrameQueue = {
    enqueue: () => void;
    cancel: () => void;
};

function readFraction(scrollY: number, total: number): number {
    return total > 0 ? Math.min(1, Math.max(0, scrollY / total)) : 0;
}

function createFrameQueue(run: () => void): FrameQueue {
    let queued = false;
    let unsubscribe: (() => void) | null = null;

    const frame = (): boolean => {
        queued = false;
        unsubscribe = null;
        run();
        return false;
    };

    return {
        enqueue() {
            if (queued) return;
            queued = true;
            unsubscribe = subscribe(frame);
        },
        cancel() {
            unsubscribe?.();
            queued = false;
        },
    };
}

export function mountReadingProgress(fill: HTMLElement): () => void {
    if (typeof window === 'undefined') return () => {};

    const doc = document.documentElement;
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(() => onResize());
    let total = 0;
    let updates = 0;

    const measure = (): void => {
        total = doc.scrollHeight - window.innerHeight;
    };

    const paint = (): void => {
        if (!observer && updates++ % FALLBACK_REMEASURE_EVERY === 0) measure();
        fill.style.setProperty('--read', readFraction(window.scrollY, total).toFixed(4));
    };

    const queue = createFrameQueue(paint);

    function onResize(): void {
        measure();
        queue.enqueue();
    }

    window.addEventListener('scroll', queue.enqueue, { passive: true });
    window.addEventListener('resize', onResize, { passive: true });
    observer?.observe(doc);
    measure();
    paint();

    return () => {
        window.removeEventListener('scroll', queue.enqueue);
        window.removeEventListener('resize', onResize);
        observer?.disconnect();
        queue.cancel();
    };
}
