// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { copyFlash } from '@bagel/ui/lib/clipboard';

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
