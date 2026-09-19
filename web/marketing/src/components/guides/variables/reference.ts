// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Client behaviour for the variables guide: search + category/surface
// filtering, URL state (q/category/surface, unchanged from the previous
// single-list page), a deep-link hash that opens and scrolls to one entry,
// and the copy buttons. Every <details> works with none of this loaded --
// this only adds the narrowing and the URL sync on top.
import { copyFlash } from '@bagel/ui/lib/clipboard';
import { wireTryIt } from './evaluate';

interface FilterState {
    q: string;
    category: string;
    surface: string;
}

function readInitial(root: HTMLElement): FilterState {
    return {
        q: root.dataset.initialQuery ?? '',
        category: root.dataset.initialCategory ?? '',
        surface: root.dataset.initialSurface ?? '',
    };
}

function matches(entry: HTMLElement, state: FilterState): boolean {
    const needle = state.q.trim().toLocaleLowerCase();
    const haystack = entry.dataset.search ?? '';
    const surfaces = (entry.dataset.surfaces ?? '').split('|');
    return (!needle || haystack.includes(needle))
        && (!state.category || entry.dataset.category === state.category)
        && (!state.surface || surfaces.includes(state.surface));
}

function applyFilter(root: HTMLElement, state: FilterState, countEl: HTMLElement | null, showingText: string): number {
    const entries = [...root.querySelectorAll<HTMLElement>('[data-vref-entry]')];
    let visible = 0;
    for (const entry of entries) {
        const isMatch = matches(entry, state);
        entry.hidden = !isMatch;
        if (isMatch) visible++;
    }
    for (const group of root.querySelectorAll<HTMLElement>('[data-vref-group]')) {
        const hasVisible = group.querySelector('[data-vref-entry]:not([hidden])');
        group.hidden = !hasVisible;
    }
    if (countEl) countEl.textContent = `${visible} ${showingText}`;
    return visible;
}

function syncUrl(state: FilterState): void {
    const params = new URLSearchParams();
    if (state.q.trim()) params.set('q', state.q.trim());
    if (state.category) params.set('category', state.category);
    if (state.surface) params.set('surface', state.surface);
    const query = params.toString();
    history.replaceState(null, '', `${location.pathname}${query ? `?${query}` : ''}${location.hash}`);
}

function setPressed(chips: readonly HTMLButtonElement[], active: HTMLButtonElement): void {
    for (const chip of chips) chip.toggleAttribute('data-on', chip === active);
}

function wireSurfaceChips(root: HTMLElement, state: FilterState, onChange: () => void): void {
    const chips = [...root.querySelectorAll<HTMLButtonElement>('[data-vref-surface]')];
    for (const chip of chips) {
        chip.addEventListener('click', () => {
            state.surface = chip.dataset.vrefSurface ?? '';
            setPressed(chips, chip);
            onChange();
        });
    }
}

function wireCategoryLinks(root: HTMLElement, state: FilterState, onChange: () => void): void {
    for (const link of root.querySelectorAll<HTMLAnchorElement>('[data-vref-cat]')) {
        link.addEventListener('click', () => {
            state.category = state.category === link.dataset.vrefCat ? '' : (link.dataset.vrefCat ?? '');
            onChange();
        });
    }
}

/** A deep link always wins over whatever filter state it landed under: open
 * the entry, make sure nothing is hiding it, and scroll it into view. */
function openHashEntry(root: HTMLElement): void {
    const id = location.hash.slice(1);
    if (!id) return;
    const entry = root.querySelector<HTMLDetailsElement>(`#${CSS.escape(id)}[data-vref-entry]`);
    if (!entry) return;
    entry.open = true;
    entry.hidden = false;
    entry.closest<HTMLElement>('[data-vref-group]')?.removeAttribute('hidden');
    entry.scrollIntoView({ block: 'start' });
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

    const search = root.querySelector<HTMLInputElement>('[data-vref-search]');
    const count = root.querySelector<HTMLElement>('[data-vref-count]');
    const empty = root.querySelector<HTMLElement>('[data-vref-empty]');
    const copyText = JSON.parse(root.querySelector('[data-vref-copy-text]')?.textContent || '{}') as {
        showing?: string; copy?: string; copied?: string; copyFailed?: string;
    };
    const state = readInitial(root);
    if (search) search.value = state.q;

    const refresh = () => {
        const visible = applyFilter(root, state, count, copyText.showing ?? 'variables shown');
        if (empty) empty.hidden = visible !== 0;
        syncUrl(state);
    };

    search?.addEventListener('input', () => { state.q = search.value; refresh(); });
    wireCategoryLinks(root, state, refresh);
    wireSurfaceChips(root, state, refresh);
    wireCopyButtons(root, copyText.copy ?? 'Copy', copyText.copied ?? 'Copied', copyText.copyFailed ?? 'Copy failed');
    wireTryIt(root);

    window.addEventListener('popstate', () => {
        const params = new URLSearchParams(location.search);
        state.q = params.get('q') ?? '';
        state.category = params.get('category') ?? '';
        state.surface = params.get('surface') ?? '';
        if (search) search.value = state.q;
        refresh();
    });

    refresh();
    openHashEntry(root);
}

export function bootAllVariableReferences(): void {
    document.querySelectorAll<HTMLElement>('[data-vref-root]').forEach(bootVariableReference);
}
