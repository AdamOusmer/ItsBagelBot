// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Client behaviour for the variables guide: the copy buttons, plus scrolling
// a `#id` into view on load. The page is a set of static tables with real
// anchors now (no <details> left to open), but the site runs Astro's
// ClientRouter (Layout.astro's <ClientRouter />) on every page, whose
// transition does not repeat the browser's own scroll-to-fragment the way a
// plain MPA navigation would -- verified by hard-navigating to
// `/guides/variables#positional` and finding `window.scrollY` still 0. The
// search box, category/surface filters and their URL state went with the
// <details> layout they existed to filter (docs/specs/variables-catalog.md
// phase 7); this scroll is the one piece of that old deep-link opener a
// static table still needs.
import { copyFlash } from '@bagel/ui/lib/clipboard';

function scrollToHash(): void {
    const raw = location.hash.slice(1);
    if (!raw) return;
    // A malformed percent-escape (e.g. a stray "%" from a visitor-edited
    // URL) throws URIError; the raw hash is still a usable id to look up.
    let id = raw;
    try {
        id = decodeURIComponent(raw);
    } catch {
        // fall through with the raw hash
    }
    document.getElementById(id)?.scrollIntoView({ block: 'start' });
}

function wireCopyButtons(root: HTMLElement, copyLabel: string, copiedLabel: string, failedLabel: string): void {
    for (const button of root.querySelectorAll<HTMLButtonElement>('[data-vref-copy]')) {
        button.addEventListener('click', () => {
            const value = button.dataset.vrefCopy ?? '';
            void copyFlash(value, (on) => {
                button.textContent = on ? copiedLabel : copyLabel;
            }).catch(() => {
                button.textContent = failedLabel;
                window.setTimeout(() => { button.textContent = copyLabel; }, 1600);
            });
        });
    }
}

export function bootVariableReference(root: HTMLElement): void {
    if (root.dataset.vrefReady === 'true') return;
    root.dataset.vrefReady = 'true';

    const copyText = JSON.parse(root.querySelector('[data-vref-copy-text]')?.textContent || '{}') as {
        copy?: string; copied?: string; copyFailed?: string;
    };
    wireCopyButtons(root, copyText.copy ?? 'Copy', copyText.copied ?? 'Copied', copyText.copyFailed ?? 'Copy failed');
    scrollToHash();
}

export function bootAllVariableReferences(): void {
    document.querySelectorAll<HTMLElement>('[data-vref-root]').forEach(bootVariableReference);
}
