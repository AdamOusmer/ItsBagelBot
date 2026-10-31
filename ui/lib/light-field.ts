// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';
import { subscribe, wake } from './raf-loop';

type Mote = {
    x: number;
    y: number;
    r: number;
    vx: number;
    vy: number;
    fill: string;
};

export type FieldOptions = {
    warmth?: number;
};

const FRAME_MS = 1000 / 60;
const MAX_STEP_MS = 100;
const EDGE_PX = 10;
const MAX_DPR = 2;
const RESIZE_DEBOUNCE_MS = 150;
const TAN_RGB = '216, 191, 145';
const GREEN_GLOW_RGB = '98, 165, 147';

function channel(host: HTMLElement, name: string, fallback: string): string {
    return getComputedStyle(host).getPropertyValue(name).trim() || fallback;
}

export function field(host: HTMLElement, options: FieldOptions = {}): (() => void) | null {
    if (prefersReducedMotion()) return null;
    const ctx = createCanvas(host);
    if (!ctx) return null;

    const warmth = options.warmth ?? 0.7;
    const warmRgb = channel(host, '--bb-tan-rgb', TAN_RGB);
    const coolRgb = channel(host, '--bb-green-glow-rgb', GREEN_GLOW_RGB);
    const canvas = ctx.canvas;
    let width = 0;
    let height = 0;
    let motes: Mote[] = [];
    let last = 0;
    let visible = false;
    let rebuild: ReturnType<typeof setTimeout> | undefined;

    function build() {
        width = host.clientWidth;
        height = host.clientHeight;
        const dpr = Math.min(window.devicePixelRatio || 1, MAX_DPR);
        canvas.width = Math.round(width * dpr);
        canvas.height = Math.round(height * dpr);
        ctx!.setTransform(dpr, 0, 0, dpr, 0, 0);
        motes = width && height ? makeMotes(width, height, warmth, warmRgb, coolRgb) : [];
    }

    function tick(now: number): boolean | void {
        if (!visible) return false;
        const dt = last ? Math.min(now - last, MAX_STEP_MS) : 0;
        last = now;
        step(motes, dt / FRAME_MS, width, height);
        draw(ctx!, motes, width, height);
    }

    const resized = () => host.clientWidth !== width || host.clientHeight !== height;

    const observer = new IntersectionObserver(([entry]) => {
        visible = entry.isIntersecting;
        if (!visible) return;
        if (resized()) build();
        last = 0;
        wake(tick);
    }, { rootMargin: '150px' });

    const resizer = new ResizeObserver(() => {
        clearTimeout(rebuild);
        rebuild = setTimeout(() => {
            if (visible && resized()) build();
        }, RESIZE_DEBOUNCE_MS);
    });

    const unsubscribe = subscribe(tick);
    observer.observe(host);
    resizer.observe(host);

    return () => {
        observer.disconnect();
        resizer.disconnect();
        clearTimeout(rebuild);
        unsubscribe();
        canvas.remove();
    };
}

function createCanvas(host: HTMLElement): CanvasRenderingContext2D | null {
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext?.('2d') ?? null;
    if (!ctx) return null;
    canvas.setAttribute('aria-hidden', 'true');
    canvas.style.cssText = 'position:absolute;inset:0;width:100%;height:100%';
    host.replaceChildren(canvas);
    return ctx;
}

function step(motes: Mote[], frames: number, width: number, height: number) {
    for (const mote of motes) {
        mote.x += mote.vx * frames;
        mote.y -= mote.vy * frames;
        if (mote.y < -EDGE_PX - mote.r) {
            mote.y = height + EDGE_PX + mote.r;
            mote.x = Math.random() * width;
        }
    }
}

function draw(ctx: CanvasRenderingContext2D, motes: Mote[], width: number, height: number) {
    ctx.clearRect(0, 0, width, height);
    for (const mote of motes) {
        ctx.fillStyle = mote.fill;
        ctx.beginPath();
        ctx.arc(mote.x, mote.y, mote.r, 0, Math.PI * 2);
        ctx.fill();
    }
}

function makeMotes(width: number, height: number, warmth: number, warmRgb: string, coolRgb: string): Mote[] {
    const count = width < 700 ? 40 : 70;
    return Array.from({ length: count }, () => ({
        x: Math.random() * width,
        y: Math.random() * height,
        r: 0.6 + Math.random() * 2,
        vy: 0.05 + Math.random() * 0.2,
        vx: (Math.random() - 0.5) * 0.1,
        fill: `rgba(${Math.random() < warmth ? warmRgb : coolRgb}, ${(0.12 + Math.random() * 0.45).toFixed(3)})`,
    }));
}
