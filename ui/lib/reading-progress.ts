// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { subscribe } from './raf-loop';

export function mountReadingProgress(fill: HTMLElement): () => void {
    if (typeof window === 'undefined') return () => {};

    let queued = false;
    let unsubscribe: (() => void) | null = null;

    const update = (): boolean => {
        queued = false;
        unsubscribe = null;
        const doc = document.documentElement;
        const total = doc.scrollHeight - window.innerHeight;
        const read = total > 0 ? Math.min(1, Math.max(0, window.scrollY / total)) : 0;
        fill.style.setProperty('--read', read.toFixed(4));
        return false;
    };

    const onScroll = (): void => {
        if (queued) return;
        queued = true;
        unsubscribe = subscribe(update);
    };

    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onScroll, { passive: true });
    update();

    return () => {
        window.removeEventListener('scroll', onScroll);
        window.removeEventListener('resize', onScroll);
        unsubscribe?.();
        queued = false;
    };
}
