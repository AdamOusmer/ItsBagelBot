// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

function scrollToHash(): void {
    const raw = location.hash.slice(1);
    if (!raw) return;
    let id = raw;
    try {
        id = decodeURIComponent(raw);
    } catch {
    }
    document.getElementById(id)?.scrollIntoView({ block: 'start' });
}

export function bootVariableReference(root: HTMLElement): void {
    if (root.dataset.vrefReady === 'true') return;
    root.dataset.vrefReady = 'true';
    scrollToHash();
}

export function bootAllVariableReferences(): void {
    document.querySelectorAll<HTMLElement>('[data-vref-root]').forEach(bootVariableReference);
}
