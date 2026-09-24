// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';

const SELECTOR = '[data-reveal]';
const REVEALED = 'is-revealed';

export type RevealOptions = {
    rootMargin?: string;
    threshold?: number;
};

const DEFAULTS: Required<RevealOptions> = {
    rootMargin: '0px 0px -10% 0px',
    threshold: 0.08,
};

function onIntersect(entries: IntersectionObserverEntry[], observer: IntersectionObserver): void {
    for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const el = entry.target as HTMLElement;
        el.classList.add(REVEALED);
        if (el.dataset.revealRepeat !== 'true') observer.unobserve(el);
    }
}

function targetsIn(root: ParentNode): HTMLElement[] {
    const found = Array.from(root.querySelectorAll<HTMLElement>(SELECTOR));
    if (root instanceof HTMLElement && root.matches(SELECTOR)) found.unshift(root);
    return found.filter((el) => el.dataset.revealReady !== 'true');
}

function onScreen(el: HTMLElement): boolean {
    const rect = el.getBoundingClientRect();
    return rect.top < window.innerHeight && rect.bottom > 0;
}

export function observeReveal(root: ParentNode, options: RevealOptions = {}): () => void {
    const { rootMargin, threshold } = { ...DEFAULTS, ...options };
    const targets = targetsIn(root);
    for (const el of targets) el.dataset.revealReady = 'true';

    const release = (): void => {
        for (const el of targets) delete el.dataset.revealReady;
    };

    if (prefersReducedMotion() || typeof IntersectionObserver === 'undefined') {
        for (const el of targets) el.classList.add(REVEALED);
        return release;
    }

    const observer = new IntersectionObserver(onIntersect, { rootMargin, threshold });
    for (const el of targets) {
        if (onScreen(el)) el.classList.add(REVEALED);
        else observer.observe(el);
    }

    return () => {
        observer.disconnect();
        release();
    };
}
