// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';

const SELECTOR = '.reveal-in';
const BASE = 'reveal-in';
const PENDING = 'is-pending';
const REVEALED = 'is-revealed';
const REPEAT = 'reveal-in--repeat';

export type RevealOptions = {
    rootMargin?: string;
    threshold?: number;
    instant?: boolean;
};

const DEFAULTS: Required<RevealOptions> = {
    rootMargin: '0px 0px -10% 0px',
    threshold: 0,
    instant: false,
};

const handled = new WeakSet<HTMLElement>();

function nestedIn(el: HTMLElement): HTMLElement[] {
    return Array.from(el.querySelectorAll<HTMLElement>(SELECTOR));
}

function show(el: HTMLElement, observer: IntersectionObserver): void {
    el.classList.remove(PENDING);
    el.classList.add(REVEALED);
    if (!el.classList.contains(REPEAT)) observer.unobserve(el);
}

function onIntersect(entries: IntersectionObserverEntry[], observer: IntersectionObserver): void {
    for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const el = entry.target as HTMLElement;
        show(el, observer);
        for (const nested of nestedIn(el)) show(nested, observer);
    }
}

function targetsIn(root: ParentNode): HTMLElement[] {
    const found = Array.from(root.querySelectorAll<HTMLElement>(SELECTOR));
    if (root instanceof HTMLElement && root.matches(SELECTOR)) found.unshift(root);
    return found.filter((el) => !handled.has(el));
}

function revealsInstantly(instant: boolean): boolean {
    return instant || prefersReducedMotion() || typeof IntersectionObserver === 'undefined';
}

function skipped(el: HTMLElement): boolean {
    return typeof el.checkVisibility === 'function' && !el.checkVisibility({ contentVisibilityAuto: true });
}

function onScreen(el: HTMLElement): boolean {
    if (skipped(el)) return false;
    const rect = el.getBoundingClientRect();
    return rect.top < window.innerHeight && rect.bottom > 0;
}

function shownAtSetup(targets: HTMLElement[]): Set<HTMLElement> {
    const visible = targets.map(onScreen);
    const shown = new Set(targets.filter((_, i) => visible[i]));
    for (const el of [...shown]) nestedIn(el).forEach((nested) => shown.add(nested));
    return shown;
}

function settle(targets: HTMLElement[], observer: IntersectionObserver): void {
    const shown = shownAtSetup(targets);
    for (const el of targets) {
        if (shown.has(el)) {
            el.classList.add(REVEALED);
            continue;
        }
        el.classList.add(PENDING);
        observer.observe(el);
    }
}

function claim(targets: HTMLElement[]): () => void {
    for (const el of targets) handled.add(el);
    return () => {
        for (const el of targets) handled.delete(el);
    };
}

export function observeReveal(root: ParentNode, options: RevealOptions = {}): () => void {
    const { rootMargin, threshold, instant } = { ...DEFAULTS, ...options };
    const targets = targetsIn(root);
    const release = claim(targets);

    if (revealsInstantly(instant)) {
        for (const el of targets) el.classList.add(REVEALED);
        return release;
    }

    const observer = new IntersectionObserver(onIntersect, { rootMargin, threshold });
    settle(targets, observer);

    return () => {
        observer.disconnect();
        release();
    };
}

type Shared = { observer: IntersectionObserver; users: number };

const shared = new WeakMap<Document, Shared>();

function sharedFor(doc: Document): Shared {
    let entry = shared.get(doc);
    if (!entry) {
        const { rootMargin, threshold } = DEFAULTS;
        entry = { observer: new IntersectionObserver(onIntersect, { rootMargin, threshold }), users: 0 };
        shared.set(doc, entry);
    }
    return entry;
}

function leave(doc: Document, node: HTMLElement): void {
    const entry = shared.get(doc);
    if (!entry) return;
    entry.observer.unobserve(node);
    if (--entry.users > 0) return;
    entry.observer.disconnect();
    shared.delete(doc);
}

export function reveal(node: HTMLElement, options: Pick<RevealOptions, 'instant'> = {}): { destroy(): void } {
    node.classList.add(BASE);
    if (handled.has(node)) return { destroy() {} };
    const release = claim([node]);
    if (revealsInstantly(options.instant ?? false)) {
        node.classList.add(REVEALED);
        return { destroy: release };
    }
    const doc = node.ownerDocument;
    const entry = sharedFor(doc);
    entry.users++;
    settle([node], entry.observer);
    return {
        destroy() {
            leave(doc, node);
            release();
        },
    };
}
