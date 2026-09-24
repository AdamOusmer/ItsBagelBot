// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';
import { subscribe } from './raf-loop';

const MAX_DEVICE_PIXEL_RATIO = 2;

type Mote = {
    x: number;
    y: number;
    r: number;
    vx: number;
    vy: number;
    alpha: number;
    warm: boolean;
};

export type FieldOptions = {
    warmth?: number;
};

export function field(canvas: HTMLCanvasElement, options: FieldOptions = {}): (() => void) | null {
    if (prefersReducedMotion()) return null;
    const ctx = canvas.getContext('2d');
    if (!ctx) return null;

    const warmth = options.warmth ?? 0.7;
    let width = 0;
    let height = 0;
    let dpr = devicePixelRatio();
    let motes: Mote[] = [];
    let unsubscribe: (() => void) | null = null;

    function build() {
        width = canvas.clientWidth;
        height = canvas.clientHeight;
        if (!width || !height) return;
        canvas.width = Math.round(width * dpr);
        canvas.height = Math.round(height * dpr);
        motes = makeMotes(width, height, warmth);
    }

    function draw() {
        if (!width || !height) build();
        if (!width || !height) return;
        ctx!.setTransform(dpr, 0, 0, dpr, 0, 0);
        ctx!.clearRect(0, 0, width, height);
        ctx!.globalCompositeOperation = 'lighter';
        for (const mote of motes) paint(ctx!, advance(mote, width, height));
        ctx!.globalCompositeOperation = 'source-over';
    }

    function start() {
        if (unsubscribe) return;
        unsubscribe = subscribe(() => {
            draw();
        });
    }

    function stop() {
        if (!unsubscribe) return;
        unsubscribe();
        unsubscribe = null;
    }

    const observer = new IntersectionObserver(([entry]) => {
        if (entry.isIntersecting) start();
        else stop();
    }, { rootMargin: '150px' });

    const resize = () => {
        dpr = devicePixelRatio();
        build();
    };

    build();
    observer.observe(canvas);
    window.addEventListener('resize', resize, { passive: true });

    return () => {
        stop();
        observer.disconnect();
        window.removeEventListener('resize', resize);
    };
}

function devicePixelRatio(): number {
    return Math.min(window.devicePixelRatio || 1, MAX_DEVICE_PIXEL_RATIO);
}

function makeMotes(width: number, height: number, warmth: number): Mote[] {
    const count = width < 700 ? 40 : 70;
    return Array.from({ length: count }, () => ({
        x: Math.random() * width,
        y: Math.random() * height,
        r: 0.6 + Math.random() * 2,
        vy: -(0.05 + Math.random() * 0.2),
        vx: (Math.random() - 0.5) * 0.1,
        alpha: 0.12 + Math.random() * 0.45,
        warm: Math.random() < warmth,
    }));
}

function advance(mote: Mote, width: number, height: number): Mote {
    mote.y += mote.vy;
    mote.x += mote.vx;
    if (mote.y < -10) {
        mote.y = height + 10;
        mote.x = Math.random() * width;
    }
    if (mote.x < -10) mote.x = width + 10;
    else if (mote.x > width + 10) mote.x = -10;
    return mote;
}

function paint(ctx: CanvasRenderingContext2D, mote: Mote): void {
    ctx.beginPath();
    ctx.arc(mote.x, mote.y, mote.r, 0, Math.PI * 2);
    ctx.fillStyle = `rgba(${mote.warm ? '201, 168, 124' : '82, 183, 136'}, ${mote.alpha.toFixed(3)})`;
    ctx.fill();
}
