// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';

type Mote = {
    x: number;
    y: number;
    r: number;
    fill: string;
};

type Layer = {
    sway: HTMLElement;
    canvas: HTMLCanvasElement;
    ctx: CanvasRenderingContext2D;
    speed: number;
    driftSeed: number;
    swaySeed: number;
    swayS: number;
};

type Size = {
    width: number;
    height: number;
    dpr: number;
};

type Palette = {
    warmth: number;
    warmRgb: string;
    coolRgb: string;
};

export type FieldOptions = {
    warmth?: number;
};

const SPEEDS_PX_PER_FRAME = [0.07, 0.14, 0.24];
const FPS = 60;
const MAX_DPR = 2;
const RESIZE_DEBOUNCE_MS = 150;
const SWAY_MIN_S = 20;
const SWAY_SPAN_S = 20;
const TAN_RGB = '216, 191, 145';
const GREEN_GLOW_RGB = '98, 165, 147';

function channel(host: HTMLElement, name: string, fallback: string): string {
    return getComputedStyle(host).getPropertyValue(name).trim() || fallback;
}

export function field(host: HTMLElement, options: FieldOptions = {}): (() => void) | null {
    if (prefersReducedMotion()) return null;
    const layers = SPEEDS_PX_PER_FRAME.map(createLayer);
    if (layers.some((layer) => !layer)) return null;
    const all = layers as Layer[];
    host.replaceChildren(...all.map((layer) => layer.sway));

    const warmth = options.warmth ?? 0.7;
    const warmRgb = channel(host, '--bb-tan-rgb', TAN_RGB);
    const coolRgb = channel(host, '--bb-green-glow-rgb', GREEN_GLOW_RGB);
    let width = 0;
    let height = 0;
    let rebuild: ReturnType<typeof setTimeout> | undefined;

    function build() {
        width = host.clientWidth;
        height = host.clientHeight;
        if (!width || !height) return;
        const dpr = Math.min(window.devicePixelRatio || 1, MAX_DPR);
        const size = { width, height, dpr };
        const motes = makeMotes(size, { warmth, warmRgb, coolRgb });
        all.forEach((layer, i) => paintLayer(layer, motes.filter((_, n) => n % all.length === i), size));
    }

    const resizer = new ResizeObserver(() => {
        clearTimeout(rebuild);
        rebuild = setTimeout(() => {
            if (host.clientWidth !== width || host.clientHeight !== height) build();
        }, RESIZE_DEBOUNCE_MS);
    });

    build();
    resizer.observe(host);

    return () => {
        resizer.disconnect();
        clearTimeout(rebuild);
        all.forEach((layer) => layer.sway.remove());
    };
}

function createLayer(speed: number): Layer | null {
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext?.('2d') ?? null;
    if (!ctx) return null;
    const sway = document.createElement('div');
    sway.className = 'bb-light-field__layer';
    canvas.className = 'bb-light-field__canvas';
    canvas.setAttribute('aria-hidden', 'true');
    sway.append(canvas);
    return { sway, canvas, ctx, speed, driftSeed: Math.random(), swaySeed: Math.random(), swayS: SWAY_MIN_S + Math.random() * SWAY_SPAN_S };
}

function paintLayer(layer: Layer, motes: Mote[], { width, height, dpr }: Size) {
    const { canvas, ctx } = layer;
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(height * 2 * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height * 2);
    for (const mote of motes) {
        ctx.fillStyle = mote.fill;
        for (const offset of [-height, 0, height]) dot(ctx, mote, offset);
    }
    const drift = height / layer.speed / FPS;
    canvas.style.animationDuration = `${drift.toFixed(2)}s`;
    canvas.style.animationDelay = `${(-layer.driftSeed * drift).toFixed(2)}s`;
    layer.sway.style.animationDuration = `${layer.swayS.toFixed(2)}s`;
    layer.sway.style.animationDelay = `${(-layer.swaySeed * layer.swayS).toFixed(2)}s`;
}

function dot(ctx: CanvasRenderingContext2D, mote: Mote, offset: number) {
    ctx.beginPath();
    ctx.arc(mote.x, mote.y + offset, mote.r, 0, Math.PI * 2);
    ctx.fill();
}

function makeMotes({ width, height }: Size, { warmth, warmRgb, coolRgb }: Palette): Mote[] {
    const count = width < 700 ? 40 : 70;
    return Array.from({ length: count }, () => ({
        x: Math.random() * width,
        y: Math.random() * height,
        r: 0.6 + Math.random() * 2,
        fill: `rgba(${Math.random() < warmth ? warmRgb : coolRgb}, ${(0.12 + Math.random() * 0.45).toFixed(3)})`,
    }));
}
