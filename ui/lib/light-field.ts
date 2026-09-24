// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';

type Mote = {
    r: number;
    vx: number;
    vy: number;
    alpha: number;
    warm: boolean;
};

export type FieldOptions = {
    warmth?: number;
};

const FRAME_MS = 1000 / 60;
const EDGE_PX = 10;

export function field(host: HTMLElement, options: FieldOptions = {}): (() => void) | null {
    if (prefersReducedMotion()) return null;
    if (typeof host.animate !== 'function') return null;

    const warmth = options.warmth ?? 0.7;
    let width = 0;
    let height = 0;
    let visible = false;
    const animations = new Set<Animation>();

    function clear() {
        for (const animation of animations) animation.cancel();
        animations.clear();
        host.replaceChildren();
    }

    function fly(dot: HTMLElement, mote: Mote, fromY: number) {
        const x = Math.random() * width - mote.r;
        const frames = (fromY + EDGE_PX) / mote.vy;
        const animation = dot.animate(
            [
                { transform: `translate(${x}px, ${fromY - mote.r}px)` },
                { transform: `translate(${x + mote.vx * frames}px, ${-EDGE_PX - mote.r}px)` },
            ],
            { duration: frames * FRAME_MS, easing: 'linear' },
        );
        animations.add(animation);
        animation.onfinish = () => {
            animations.delete(animation);
            fly(dot, mote, height + EDGE_PX);
        };
    }

    function build() {
        clear();
        width = host.clientWidth;
        height = host.clientHeight;
        if (!width || !height) return;
        for (const mote of makeMotes(width, warmth)) {
            const dot = document.createElement('span');
            dot.style.width = dot.style.height = `${mote.r * 2}px`;
            dot.style.background = `rgba(${mote.warm ? '201, 168, 124' : '82, 183, 136'}, ${mote.alpha.toFixed(3)})`;
            host.append(dot);
            fly(dot, mote, Math.random() * height);
        }
    }

    const observer = new IntersectionObserver(([entry]) => {
        visible = entry.isIntersecting;
        if (visible) build();
        else clear();
    }, { rootMargin: '150px' });

    const resized = () => host.clientWidth !== width || host.clientHeight !== height;

    const resizer = new ResizeObserver(() => {
        if (visible && resized()) build();
    });

    observer.observe(host);
    resizer.observe(host);

    return () => {
        observer.disconnect();
        resizer.disconnect();
        clear();
    };
}

function makeMotes(width: number, warmth: number): Mote[] {
    const count = width < 700 ? 40 : 70;
    return Array.from({ length: count }, () => ({
        r: 0.6 + Math.random() * 2,
        vy: 0.05 + Math.random() * 0.2,
        vx: (Math.random() - 0.5) * 0.1,
        alpha: 0.12 + Math.random() * 0.45,
        warm: Math.random() < warmth,
    }));
}
