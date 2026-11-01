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

export function mountReadingProgress(fill: HTMLElement): () => void {
    if (typeof window === 'undefined') return () => {};

    const doc = document.documentElement;
    let docHeight = doc.scrollHeight;
    let total = 0;
    let updates = 0;

    const measure = (): void => {
        total = docHeight - window.innerHeight;
    };

    // Resize must not read layout: iOS fires it on every step of the address bar animation.
    const observer =
        typeof ResizeObserver === 'undefined'
            ? null
            : new ResizeObserver(([entry]) => {
                  docHeight = entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height;
                  onResize();
              });

    const paint = (): void => {
        if (!observer && updates++ % FALLBACK_REMEASURE_EVERY === 0) {
            docHeight = doc.scrollHeight;
            measure();
        }
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
