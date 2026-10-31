// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const OFFSTAGE = 'is-offstage';
const DEFAULT_SELECTOR = 'main > header, main > section, main > div > section, main > .seam, footer';

export function watchOffstage(root: ParentNode = document, selector = DEFAULT_SELECTOR): () => void {
    if (typeof IntersectionObserver === 'undefined') return () => {};

    const targets = Array.from(root.querySelectorAll<HTMLElement>(selector));
    const observer = new IntersectionObserver(
        (entries) => {
            for (const entry of entries) entry.target.classList.toggle(OFFSTAGE, !entry.isIntersecting);
        },
        { rootMargin: '25% 0px 25% 0px' },
    );
    for (const el of targets) observer.observe(el);

    return () => {
        observer.disconnect();
        for (const el of targets) el.classList.remove(OFFSTAGE);
    };
}
