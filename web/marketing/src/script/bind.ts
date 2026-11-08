// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function bindOnce(selector: string, setup: (root: HTMLElement) => void): void {
    const ready = new WeakSet<HTMLElement>();
    function bindAll() {
        document.querySelectorAll<HTMLElement>(selector).forEach((root) => {
            if (ready.has(root)) return;
            setup(root);
            ready.add(root);
        });
    }

    bindAll();
    document.addEventListener('astro:page-load', bindAll);
}
