// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function bindOnce(selector: string, setup: (root: HTMLElement) => void): void {
    function bindAll() {
        document.querySelectorAll<HTMLElement>(selector).forEach((root) => {
            if (root.dataset.ready === '1') return;
            root.dataset.ready = '1';
            setup(root);
        });
    }

    bindAll();
    document.addEventListener('astro:page-load', bindAll);
}
