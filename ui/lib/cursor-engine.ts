// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { finePointer, reduceMotion } from './motion-query';
import { subscribe, wake } from './raf-loop';

export type CursorEase = {
    hover: number;
    release: number;
};

export type CursorOptions = {
    dot: HTMLElement;
    ring: HTMLElement;
    selector?: string;
    quietAttr?: string;
    ease?: CursorEase;
};

const SELECTOR = 'a, button, .bb-input, [data-cursor]';
const QUIET = '[data-cursor="quiet"]';
const TEXT =
    '.bb-input:not(.bb-input--select), textarea, [contenteditable]:not([contenteditable="false"]), ' +
    'input:not([type="checkbox"], [type="radio"], [type="range"], [type="color"], [type="file"], ' +
    '[type="button"], [type="submit"], [type="reset"], [type="image"], [type="hidden"])';
const EASE: CursorEase = { hover: 0.3, release: 0.28 };

const LIGHT_LUMA = 0.5;

const IDLE_SIZE = 36;
const HOVER_PAD = 6;
const FALLBACK_RADIUS = 8;
const SETTLE_PX = 0.3;
const IDLE_MS = 500;

let pointerX = -1;
let pointerY = -1;

type Box = { x: number; y: number; w: number; h: number; r: number };

const lerp = (a: number, b: number, t: number): number => a + (b - a) * t;

function idleBox(x: number, y: number): Box {
    const half = IDLE_SIZE / 2;
    return { x: x - half, y: y - half, w: IDLE_SIZE, h: IDLE_SIZE, r: half };
}

function hoverBox(el: HTMLElement): Box {
    const bounds = el.getBoundingClientRect();
    const h = bounds.height + HOVER_PAD * 2;
    const radius = parseFloat(getComputedStyle(el).borderRadius) || FALLBACK_RADIUS;
    return {
        x: bounds.left - HOVER_PAD,
        y: bounds.top - HOVER_PAD,
        w: bounds.width + HOVER_PAD * 2,
        h,
        r: Math.min(radius + HOVER_PAD, h / 2),
    };
}

function lerpBox(from: Box, to: Box, e: number): Box {
    return {
        x: lerp(from.x, to.x, e),
        y: lerp(from.y, to.y, e),
        w: lerp(from.w, to.w, e),
        h: lerp(from.h, to.h, e),
        r: lerp(from.r, to.r, e),
    };
}

function paintRing(ring: HTMLElement, box: Box, hovering: boolean, text: boolean): void {
    ring.style.transform = `translate(${box.x.toFixed(1)}px, ${box.y.toFixed(1)}px)`;
    ring.style.width = `${box.w.toFixed(1)}px`;
    ring.style.height = `${box.h.toFixed(1)}px`;
    ring.style.borderRadius = `${box.r.toFixed(1)}px`;
    ring.classList.toggle('is-morphed', hovering);
    ring.classList.toggle('is-text', text);
}

function arrived(box: Box, to: Box): boolean {
    return Math.abs(box.x - to.x) < SETTLE_PX && Math.abs(box.y - to.y) < SETTLE_PX;
}

type Surface = { backgroundColor: string; backgroundImage: string };

function colors(value: string): number[][] {
    return [...value.matchAll(/rgba?\(([^)]+)\)/g)].map((match) => {
        const [r, g, b, a = 1] = match[1].split(/[\s,/]+/).filter(Boolean).map(Number);
        return [r, g, b, a];
    });
}

const opaque = (color: number[]): boolean => color[3] >= 0.5;
const light = ([r, g, b]: number[]): boolean => (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255 > LIGHT_LUMA;

export function isLightSurface(chain: Iterable<Surface>): boolean {
    for (const style of chain) {
        const stops = colors(style.backgroundImage).filter(opaque);
        if (stops.length) return stops.some(light);
        const fill = colors(style.backgroundColor).find(opaque);
        if (fill) return light(fill);
    }
    return false;
}

function* surfaces(start: Element | null): Generator<Surface> {
    for (let el = start; el; el = el.parentElement) yield getComputedStyle(el);
}

export function mountCursor(options: CursorOptions): () => void {
    const { dot, ring, selector = SELECTOR, quietAttr = QUIET, ease = EASE } = options;
    if (!finePointer.matches) return () => {};

    if (pointerX < 0) {
        pointerX = window.innerWidth / 2;
        pointerY = window.innerHeight / 2;
    }

    let box = idleBox(pointerX, pointerY);
    let target: HTMLElement | null = null;
    let overText = false;
    let lastMove = 0;

    function hovered(): HTMLElement | null {
        return target?.isConnected ? target : null;
    }

    function paint(el: HTMLElement | null): Box {
        dot.style.transform = `translate(${pointerX}px, ${pointerY}px)`;
        dot.style.opacity = el && !overText ? '0' : '1';
        dot.classList.toggle('is-text', overText);

        const to = el ? hoverBox(el) : idleBox(pointerX, pointerY);
        box = lerpBox(box, to, el ? ease.hover : ease.release);
        paintRing(ring, box, !!el, overText);
        return to;
    }

    function tick(now: number): boolean | void {
        const el = hovered();
        const to = paint(el);
        if (reduceMotion.matches) return false;
        if (el) return;
        if (arrived(box, to) && now - lastMove > IDLE_MS) return false;
    }

    const nearest = (event: PointerEvent): HTMLElement | null =>
        (event.target as Element | null)?.closest<HTMLElement>(selector) ?? null;

    const onMove = (event: PointerEvent): void => {
        if (event.pointerType === 'touch') return;
        pointerX = event.clientX;
        pointerY = event.clientY;
        lastMove = performance.now();
        wake(tick);
    };

    const onOver = (event: PointerEvent): void => {
        dot.classList.toggle('is-inverted', isLightSurface(surfaces(event.target as Element | null)));
        const hit = (event.target as Element | null)?.closest(`${selector}, ${TEXT}`);
        const text = !!hit?.matches(TEXT);
        if (text !== overText) {
            overText = text;
            wake(tick);
        }
        const el = nearest(event);
        if (!el) return;
        target = el.matches(quietAttr) ? null : el;
        wake(tick);
    };

    const onOut = (event: PointerEvent): void => {
        if (!target || nearest(event) !== target) return;
        target = null;
        wake(tick);
    };

    const syncMotion = (): void => {
        document.documentElement.classList.toggle('bb-cursor-on', !reduceMotion.matches);
        wake(tick);
    };

    const unsubscribe = subscribe(tick);
    syncMotion();
    reduceMotion.addEventListener('change', syncMotion);
    window.addEventListener('pointermove', onMove, { passive: true });
    document.addEventListener('pointerover', onOver, { passive: true });
    document.addEventListener('pointerout', onOut, { passive: true });

    return () => {
        unsubscribe();
        reduceMotion.removeEventListener('change', syncMotion);
        window.removeEventListener('pointermove', onMove);
        document.removeEventListener('pointerover', onOver);
        document.removeEventListener('pointerout', onOut);
        document.documentElement.classList.remove('bb-cursor-on');
    };
}
