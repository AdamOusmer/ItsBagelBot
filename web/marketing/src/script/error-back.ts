// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function mountErrorBack(): void {
    document.addEventListener('click', (event) => {
        const target = event.target;
        if (!(target instanceof Element)) return;
        const back = target.closest<HTMLElement>('[data-error-back]');
        if (!back) return;
        if (window.history.length > 1) window.history.back();
        else window.location.assign(back.dataset.errorBack || '/');
    });
}
