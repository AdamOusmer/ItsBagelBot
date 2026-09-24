// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { mountScrollSpy } from '@bagel/ui/lib/scroll-spy';

export function scrollspy(prefix: string): void {
    let disposers: Array<() => void> = [];
    const teardown = () => {
        disposers.forEach((dispose) => dispose());
        disposers = [];
    };
    const setup = () => {
        teardown();
        document.querySelectorAll<HTMLElement>(`[data-${prefix}]`).forEach((root) => {
            const sections = Array.from(root.querySelectorAll<HTMLElement>(`[data-${prefix}-section]`));
            const links = new Map(
                Array.from(root.querySelectorAll<HTMLAnchorElement>(`[data-${prefix}-link]`))
                    .map((link) => [link.getAttribute(`data-${prefix}-link`) ?? '', link]),
            );
            disposers.push(mountScrollSpy(sections, links));
        });
    };
    setup();
    document.addEventListener('astro:page-load', setup);
    document.addEventListener('astro:before-swap', teardown);
    window.addEventListener('pagehide', teardown);
    window.addEventListener('pageshow', (event) => { if (event.persisted) setup(); });
}
