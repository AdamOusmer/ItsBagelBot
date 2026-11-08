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

const ready = new WeakSet<HTMLElement>();

export function bootVariableReference(root: HTMLElement): void {
    if (ready.has(root)) return;
    ready.add(root);
    scrollToHash();
}

export function bootAllVariableReferences(): void {
    document.querySelectorAll<HTMLElement>('.vref').forEach(bootVariableReference);
}
