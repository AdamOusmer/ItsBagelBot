// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { bindOnce } from './bind';

/**
 * TOC scrollspy for the long-form shells (guides, legal). Both had the same
 * 42-line copy; the only thing that ever differed between them was the
 * data-attribute prefix, so that is the parameter.
 *
 * Contract for a caller with prefix `p`: the shell root carries `data-p`, each
 * section carries `data-p-section` plus an `id`, and each TOC anchor carries
 * `data-p-link="<that id>"`. The current link gets `.is-current`.
 *
 * Attributes rather than passing element lists in: the shell renders its TOC
 * and its sections from the same content array, so the markup already names
 * the pairing and a second, JS-side list could disagree with it.
 */
export function scrollspy(prefix: string): void {
    bindOnce(`[data-${prefix}]`, (root) => setupSpy(root, prefix));
}

function setupSpy(root: HTMLElement, prefix: string): void {
    const sections = Array.from(root.querySelectorAll<HTMLElement>(`[data-${prefix}-section]`));
    const links = new Map(
        Array.from(root.querySelectorAll<HTMLAnchorElement>(`[data-${prefix}-link]`))
            .map((a) => [a.getAttribute(`data-${prefix}-link`) ?? '', a]),
    );

    let ticking = false;

    function update() {
        ticking = false;
        const currentId = currentSection(sections);
        links.forEach((link, id) => link.classList.toggle('is-current', id === currentId));
    }

    function onScroll() {
        if (ticking) return;
        ticking = true;
        requestAnimationFrame(update);
    }

    // One shared scroll listener per shell, coalesced to a frame: the read is
    // getBoundingClientRect on every section, which is a forced layout if it
    // runs per scroll event. IntersectionObserver was the obvious alternative
    // and does not fit: "current" here is the LAST section past the line, not
    // whichever ones happen to be on screen, so the answer depends on sections
    // the observer would report as not intersecting.
    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onScroll, { passive: true });
    update();
}

/** The last section whose top has passed the upper third of the viewport. */
function currentSection(sections: readonly HTMLElement[]): string | undefined {
    let currentId = sections[0]?.id;
    for (const section of sections) {
        if (section.getBoundingClientRect().top < window.innerHeight * 0.35) {
            currentId = section.id;
        }
    }
    return currentId;
}
