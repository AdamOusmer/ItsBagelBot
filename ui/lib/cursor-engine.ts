// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { CURSOR_PROP } from './cursor-prop';
import { mediaQuery, reduceMotion } from './motion-query';
import { subscribe, wake } from './raf-loop';

export type CursorEase = {
    hover: number;
    release: number;
};

export type CursorOptions = {
    dot: HTMLElement;
    ring: HTMLElement;
    fill: HTMLElement;
    selector?: string;
    ease?: CursorEase;
};

const SELECTOR = 'a, button, .bb-input';
const EASE: CursorEase = { hover: 0.16, release: 0.28 };

const IDLE_SIZE = 36;
const HOVER_PAD = 6;
const FALLBACK_RADIUS = 8;
const SETTLE_PX = 0.1;
const FRAME_MS = 1000 / 60;
const cursorPointer = mediaQuery('(min-width: 901px) and (hover: hover) and (pointer: fine)');

let pointerX = -1;
let pointerY = -1;

type Box = { x: number; y: number; w: number; h: number; r: number };

const lerp = (a: number, b: number, t: number): number => a + (b - a) * t;

function idleBox(x: number, y: number): Box {
    const half = IDLE_SIZE / 2;
    return { x: x - half, y: y - half, w: IDLE_SIZE, h: IDLE_SIZE, r: half };
}

function radiusOf(style: CSSStyleDeclaration): number {
    return parseFloat(style.borderRadius) || FALLBACK_RADIUS;
}

function hoverBox(el: HTMLElement, radius: number): Box {
    const bounds = el.getBoundingClientRect();
    const h = bounds.height + HOVER_PAD * 2;
    return {
        x: bounds.left - HOVER_PAD,
        y: bounds.top - HOVER_PAD,
        w: bounds.width + HOVER_PAD * 2,
        h,
        r: Math.min(radius + HOVER_PAD, h / 2),
    };
}

function warpBox(box: Box): Box {
    // Only the edges nearest the pointer yield, by at most two pixels.
    const px = (pointerX - box.x) / box.w;
    const py = (pointerY - box.y) / box.h;
    const dx = px >= 0 && px <= 1 ? (px - 0.5) * 4 : 0;
    const dy = py >= 0 && py <= 1 ? (py - 0.5) * 4 : 0;
    return { ...box, x: box.x + Math.min(0, dx), y: box.y + Math.min(0, dy),
        w: box.w + Math.abs(dx), h: box.h + Math.abs(dy) };
}

function morphBox(box: Box, to: Box, factor: number): Box {
    return {
        x: lerp(box.x, to.x, factor), y: lerp(box.y, to.y, factor),
        w: lerp(box.w, to.w, factor), h: lerp(box.h, to.h, factor),
        r: lerp(box.r, to.r, factor),
    };
}

function setAttribute(el: Element, name: string, value: string): void {
    if (el.getAttribute(name) !== value) el.setAttribute(name, value);
}

function paintRing(ring: HTMLElement, box: Box, shape: Box): void {
    // Lay out the destination shape once; animate its position and scale only.
    const sx = box.w / shape.w;
    const sy = box.h / shape.h;
    const radius = Math.max(0, Math.min(box.r, box.w / 2, box.h / 2));
    const translate = `translate(${box.x.toFixed(3)}px, ${box.y.toFixed(3)}px)`;
    const scale = `scale(${sx}, ${sy})`;
    const outline = ring.firstElementChild?.firstElementChild;
    // SVG must own the scale so vector-effect keeps the stroke weight constant.
    ring.style.transform = outline ? translate : `${translate} ${scale}`;
    ring.style.width = `${shape.w}px`;
    ring.style.height = `${shape.h}px`;
    // Counter-scale the corners so a circle never stretches into an ellipse.
    ring.style.borderRadius = `${radius / sx}px / ${radius / sy}px`;
    if (outline) {
        setAttribute(outline, 'transform', scale);
        setAttribute(outline, 'rx', String(radius / sx));
        setAttribute(outline, 'ry', String(radius / sy));
    }
}

function arrived(box: Box, to: Box): boolean {
    return (['x', 'y', 'w', 'h', 'r'] as const).every((key) => Math.abs(box[key] - to[key]) < SETTLE_PX);
}

export function mountCursor(options: CursorOptions): () => void {
    const { dot, ring, fill, selector = SELECTOR, ease = EASE } = options;

    if (pointerX < 0) {
        pointerX = window.innerWidth / 2;
        pointerY = window.innerHeight / 2;
    }

    let box = idleBox(pointerX, pointerY);
    let target: HTMLElement | null = null;
    let overText = false;
    let lastFrame = 0;
    let active = false;
    let radius = FALLBACK_RADIUS;

    function setTarget(next: HTMLElement | null, style?: CSSStyleDeclaration): void {
        target = next;
        if (next && style) radius = radiusOf(style);
    }

    function hovered(): HTMLElement | null {
        return target?.isConnected ? target : null;
    }

    function paint(el: HTMLElement | null, shape: Box): void {
        dot.style.transform = `translate(${pointerX}px, ${pointerY}px)`;
        dot.style.opacity = el && !overText ? '0' : '1';
        dot.classList.toggle('is-text', overText);

        for (const layer of [fill, ring]) {
            layer.classList.toggle('is-morphed', !!el);
            layer.classList.toggle('is-text', overText);
            paintRing(layer, box, shape);
        }
    }

    function advance(shape: Box, el: HTMLElement | null, now: number): boolean {
        const to = el ? warpBox(shape) : shape;
        const elapsed = lastFrame ? now - lastFrame : FRAME_MS;
        const speed = el ? ease.hover : ease.release;
        const factor = 1 - (1 - speed) ** (elapsed / FRAME_MS);
        box = morphBox(box, to, factor);
        const done = arrived(box, to);
        if (done) box = to;
        lastFrame = done ? 0 : now;
        return done;
    }

    function tick(now: number): boolean | void {
        if (!active) return false;
        const el = hovered();
        // Read geometry before any cursor writes, avoiding a forced layout.
        const shape = el ? hoverBox(el, radius) : idleBox(pointerX, pointerY);
        const done = advance(shape, el, now);
        paint(el, shape);
        if (done) return false;
    }

    const nearest = (event: PointerEvent): HTMLElement | null => {
        for (let el = event.target as Element | null; el; el = el.parentElement) {
            if (el.matches(selector) || getComputedStyle(el).getPropertyValue(CURSOR_PROP) !== '') return el as HTMLElement;
        }
        return null;
    };

    const onMove = (event: PointerEvent): void => {
        if (!active || event.pointerType === 'touch') return;
        pointerX = event.clientX;
        pointerY = event.clientY;
        wake(tick);
    };

    const onOver = (event: PointerEvent): void => {
        if (!active || event.pointerType === 'touch') return;
        const el = nearest(event);
        const style = el ? getComputedStyle(el) : undefined;
        const mode = style?.getPropertyValue(CURSOR_PROP).trim();
        const text = mode === 'text';
        if (text !== overText) {
            overText = text;
            wake(tick);
        }
        setTarget(mode === 'quiet' ? null : el, style);
        wake(tick);
    };

    const onOut = (event: PointerEvent): void => {
        if (!active || !target) return;
        if (nearest(event) !== target) return;
        setTarget(null);
        wake(tick);
    };

    const remeasure = (): void => {
        if (active && target) wake(tick);
    };

    const layered = [fill, ring, dot].filter((el) => typeof el.showPopover === 'function');

    const raise = (): void => {
        for (const el of layered) {
            if (el.matches(':popover-open')) el.hidePopover();
            el.showPopover();
        }
    };

    const onToggle = (event: Event): void => {
        if ((event as ToggleEvent).newState !== 'open') return;
        if (layered.includes(event.target as HTMLElement)) return;
        queueMicrotask(raise);
    };

    const syncMotion = (): void => {
        active = cursorPointer.matches && !reduceMotion.matches;
        lastFrame = 0;
        document.documentElement.classList.toggle('bb-cursor-on', active);
        if (active) wake(tick);
    };

    const unsubscribe = subscribe(tick);
    for (const el of layered) el.popover = 'manual';
    raise();
    syncMotion();
    reduceMotion.addEventListener('change', syncMotion);
    cursorPointer.addEventListener('change', syncMotion);
    window.addEventListener('pointermove', onMove, { passive: true });
    window.addEventListener('scroll', remeasure, { passive: true, capture: true });
    window.addEventListener('resize', remeasure, { passive: true });
    document.addEventListener('pointerover', onOver, { passive: true });
    document.addEventListener('pointerout', onOut, { passive: true });
    document.addEventListener('beforetoggle', onToggle, true);

    return () => {
        unsubscribe();
        reduceMotion.removeEventListener('change', syncMotion);
        cursorPointer.removeEventListener('change', syncMotion);
        window.removeEventListener('pointermove', onMove);
        window.removeEventListener('scroll', remeasure, true);
        window.removeEventListener('resize', remeasure);
        document.removeEventListener('pointerover', onOver);
        document.removeEventListener('pointerout', onOut);
        document.removeEventListener('beforetoggle', onToggle, true);
        for (const el of layered) el.removeAttribute('popover');
        document.documentElement.classList.remove('bb-cursor-on');
    };
}
