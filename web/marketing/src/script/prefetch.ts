// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type Connection = { saveData?: boolean; effectiveType?: string };

const HOVER_DELAY_MS = 80;
const warmed = new Set<string>();
let hoverTimer = 0;

function isConstrained(): boolean {
    const { connection } = navigator as Navigator & { connection?: Connection };
    if (!navigator.onLine) return true;
    return Boolean(connection?.saveData) || /2g/.test(connection?.effectiveType ?? '');
}

function isOtherPage(url: URL): boolean {
    return url.origin === location.origin && url.pathname !== location.pathname;
}

function warm(anchor: HTMLAnchorElement): void {
    const url = new URL(anchor.href);
    url.hash = '';
    if (warmed.has(url.href) || !isOtherPage(url)) return;
    if (isConstrained()) return;
    warmed.add(url.href);
    // The edge answers 503 to link prefetches of uncached HTML, so this must stay a plain fetch.
    fetch(url, { priority: 'low' }).catch(() => {});
}

function hoverAnchor(target: EventTarget | null): HTMLAnchorElement | null {
    if (!(target instanceof Element)) return null;
    const anchor = target.closest<HTMLAnchorElement>('a[href]');
    return (anchor?.dataset.astroPrefetch ?? 'hover') === 'hover' ? anchor : null;
}

function onPointAt(event: Event): void {
    const anchor = hoverAnchor(event.target);
    window.clearTimeout(hoverTimer);
    if (!anchor) return;
    hoverTimer = window.setTimeout(() => warm(anchor), HOVER_DELAY_MS);
}

const inView = new IntersectionObserver((entries) => {
    for (const { target } of entries.filter((entry) => entry.isIntersecting)) {
        inView.unobserve(target);
        warm(target as HTMLAnchorElement);
    }
});

function observeViewportLinks(): void {
    document
        .querySelectorAll('a[data-astro-prefetch="viewport"]')
        .forEach((anchor) => inView.observe(anchor));
}

document.addEventListener('mouseover', onPointAt, { passive: true });
document.addEventListener('focusin', onPointAt, { passive: true });
document.addEventListener('astro:page-load', observeViewportLinks);
observeViewportLinks();
