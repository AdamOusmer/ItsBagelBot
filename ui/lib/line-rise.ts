// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';

const ARMED = 'lineRise';

function wordSpans(text: string): HTMLElement[] {
    return text.split(' ').map((word) => {
        const span = document.createElement('span');
        span.textContent = word;
        return span;
    });
}

function groupLines(layer: HTMLElement, text: string): string[] {
    const words = wordSpans(text);
    const probe = document.createElement('span');
    probe.style.display = 'block';
    words.forEach((word, i) => probe.append(...(i ? [' ', word] : [word])));
    layer.append(probe);
    const lines: string[][] = [];
    let top = Number.NaN;
    for (const word of words) {
        if (word.offsetTop !== top) lines.push([]);
        top = word.offsetTop;
        lines[lines.length - 1].push(word.textContent ?? '');
    }
    return lines.map((line) => line.join(' '));
}

function buildLayer(source: HTMLElement): HTMLElement {
    const layer = document.createElement('span');
    layer.className = 'bb-page-hero__rise';
    layer.setAttribute('aria-hidden', 'true');
    layer.setAttribute('style', source.getAttribute('style') ?? '');
    return layer;
}

function fillLines(layer: HTMLElement, lines: string[]): HTMLElement {
    layer.replaceChildren(
        ...lines.map((text, i) => {
            const line = document.createElement('span');
            line.className = 'bb-page-hero__line';
            const inner = document.createElement('span');
            inner.style.setProperty('--line-i', String(i));
            inner.textContent = text;
            line.append(inner);
            return line;
        }),
    );
    return layer.lastElementChild?.firstElementChild as HTMLElement;
}

function whenFitted(source: HTMLElement, run: () => void): () => void {
    if (source.hasAttribute('data-fit-ready')) {
        run();
        return () => {};
    }
    const observer = new MutationObserver(() => {
        if (!source.hasAttribute('data-fit-ready')) return;
        observer.disconnect();
        run();
    });
    observer.observe(source, { attributes: true, attributeFilter: ['data-fit-ready'] });
    return () => observer.disconnect();
}

function canRise(title: HTMLElement, source: HTMLElement | null): source is HTMLElement {
    return source !== null && title.dataset[ARMED] === undefined && !prefersReducedMotion();
}

export function riseLines(title: HTMLElement): () => void {
    const source = title.querySelector<HTMLElement>('[data-fit]');
    if (!canRise(title, source)) return () => {};

    let layer: HTMLElement | null = null;
    const finish = (): void => {
        layer?.remove();
        layer = null;
        source.style.removeProperty('opacity');
    };

    const cancel = whenFitted(source, () => {
        if (parseFloat(getComputedStyle(title).opacity) > 0) return;
        layer = buildLayer(source);
        title.append(layer);
        const last = fillLines(layer, groupLines(layer, source.textContent ?? ''));
        source.style.opacity = '0';
        title.dataset[ARMED] = '';
        last.addEventListener('animationend', finish, { once: true });
    });

    return () => {
        cancel();
        finish();
    };
}
