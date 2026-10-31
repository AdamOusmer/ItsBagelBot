// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { subscribe } from './raf-loop';

const FALLBACK_REMEASURE_EVERY = 20;

export function mountReadingProgress(fill: HTMLElement): () => void {
    if (typeof window === 'undefined') return () => {};

    const doc = document.documentElement;
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(() => onResize());
    let queued = false;
    let unsubscribe: (() => void) | null = null;
    let total = 0;
    let updates = 0;

    const measure = (): void => {
        total = doc.scrollHeight - window.innerHeight;
    };

    const update = (): boolean => {
        queued = false;
        unsubscribe = null;
        if (!observer && updates++ % FALLBACK_REMEASURE_EVERY === 0) measure();
        const read = total > 0 ? Math.min(1, Math.max(0, window.scrollY / total)) : 0;
        fill.style.setProperty('--read', read.toFixed(4));
        return false;
    };

    const enqueue = (): void => {
        if (queued) return;
        queued = true;
        unsubscribe = subscribe(update);
    };

    function onResize(): void {
        measure();
        enqueue();
    }

    window.addEventListener('scroll', enqueue, { passive: true });
    window.addEventListener('resize', onResize, { passive: true });
    observer?.observe(doc);
    measure();
    update();

    return () => {
        window.removeEventListener('scroll', enqueue);
        window.removeEventListener('resize', onResize);
        observer?.disconnect();
        unsubscribe?.();
        queued = false;
    };
}
