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
    image: HTMLElement;
    url: string | null;
    paints: number;
    speed: number;
    driftSeed: number;
    swaySeed: number;
    swayS: number;
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
    const all = SPEEDS_PX_PER_FRAME.map(createLayer);
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
        const motes = makeMotes(width, height, warmth, warmRgb, coolRgb);
        all.forEach((layer, i) => paintLayer(layer, motes.filter((_, n) => n % all.length === i), width, height, dpr));
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
        all.forEach((layer) => {
            layer.paints++;
            setImage(layer, null);
            layer.sway.remove();
        });
    };
}

function createLayer(speed: number): Layer {
    const sway = document.createElement('div');
    const image = document.createElement('div');
    sway.className = 'bb-light-field__layer';
    image.className = 'bb-light-field__image';
    image.setAttribute?.('aria-hidden', 'true');
    sway.append(image);
    return { sway, image, url: null, paints: 0, speed, driftSeed: Math.random(), swaySeed: Math.random(), swayS: SWAY_MIN_S + Math.random() * SWAY_SPAN_S };
}

function setImage(layer: Layer, url: string | null) {
    if (layer.url) URL.revokeObjectURL(layer.url);
    layer.url = url;
    layer.image.style.backgroundImage = url ? `url("${url}")` : '';
}

function paintLayer(layer: Layer, motes: Mote[], width: number, height: number, dpr: number) {
    const drift = height / layer.speed / FPS;
    layer.image.style.animationDuration = `${drift.toFixed(2)}s`;
    layer.image.style.animationDelay = `${(-layer.driftSeed * drift).toFixed(2)}s`;
    layer.sway.style.animationDuration = `${layer.swayS.toFixed(2)}s`;
    layer.sway.style.animationDelay = `${(-layer.swaySeed * layer.swayS).toFixed(2)}s`;

    const canvas = document.createElement('canvas');
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(height * dpr);
    const ctx = canvas.getContext?.('2d');
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    for (const mote of motes) {
        ctx.fillStyle = mote.fill;
        dot(ctx, mote, 0);
        if (mote.y + mote.r > height) dot(ctx, mote, -height);
        if (mote.y - mote.r < 0) dot(ctx, mote, height);
    }
    const paint = ++layer.paints;
    if (!canvas.toBlob) return setImage(layer, canvas.toDataURL());
    canvas.toBlob((blob) => {
        if (paint === layer.paints && blob) setImage(layer, URL.createObjectURL(blob));
    });
}

function dot(ctx: CanvasRenderingContext2D, mote: Mote, offset: number) {
    ctx.beginPath();
    ctx.arc(mote.x, mote.y + offset, mote.r, 0, Math.PI * 2);
    ctx.fill();
}

function makeMotes(width: number, height: number, warmth: number, warmRgb: string, coolRgb: string): Mote[] {
    const count = width < 700 ? 40 : 70;
    return Array.from({ length: count }, () => ({
        x: Math.random() * width,
        y: Math.random() * height,
        r: 0.6 + Math.random() * 2,
        fill: `rgba(${Math.random() < warmth ? warmRgb : coolRgb}, ${(0.12 + Math.random() * 0.45).toFixed(3)})`,
    }));
}
