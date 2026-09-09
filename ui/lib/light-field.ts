// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The drifting mote field, once. The marketing site and the console each had
 * their own copy of these physics (web/marketing/src/script/lightfield.js and
 * LightField.svelte beside this file), and "same look on both surfaces" is a
 * promise a second copy cannot keep: the two had already diverged on when a
 * mote is counted warm.
 *
 * Framework-neutral on purpose, in the same spirit as rehearsal.ts: it takes a
 * canvas and hands back a teardown, so Astro drives it from a DOM scan with
 * ClientRouter teardown and Svelte drives it from onMount, and neither shape
 * leaks into the other. Pure browser APIs, no imports, so both bundlers inline
 * it with no install step.
 *
 * Lives in the standalone @bagel/ui library at the repo root, not in web/kit,
 * because it is a design primitive with no bot knowledge in it and kit is the
 * bot-specific glue. Every consumer reaches it the same way now -- as the
 * package import `@bagel/ui/lib/light-field` -- where it used to be a Vite
 * alias for marketing and a relative path for the console.
 *
 * That cost the console images their narrow build context: they built with
 * `web/` as the whole context, and nothing above that line can be COPYed. They
 * now build from the repo root behind a per-image whitelist ignorefile
 * (web/dashboard/Containerfile.ignore) that admits `ui` and the parts of `web`
 * the SSR image needs, and nothing else -- the Go tree included. The whitelist
 * is the price of the library being extractable to its own repo later; a
 * `ui/` nested inside `web/` would have kept the old context and given up the
 * separate lockfile, the separate CI job and the clean lift-out.
 */

/** One drifting speck. */
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
    /** Share of gold (vs green) motes, 0..1. */
    warmth?: number;
};

/**
 * Start a mote field on `canvas`. Returns the teardown, or `null` when the
 * field did not start (reduced motion, or no 2D context) so a caller that
 * tracks "this node is wired" can tell the difference.
 *
 * The loop is gated on an IntersectionObserver: an off-screen canvas painting
 * 70 arcs a frame is the whole cost of this effect and none of its value. The
 * 150px root margin starts it just before it scrolls in, so it is never caught
 * mid-fade with an empty canvas.
 */
export function field(canvas: HTMLCanvasElement, options: FieldOptions = {}): (() => void) | null {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return null;
    const ctx = canvas.getContext('2d');
    if (!ctx) return null;

    const warmth = options.warmth ?? 0.7;
    let width = 0;
    let height = 0;
    let dpr = devicePixelRatio();
    let motes: Mote[] = [];
    let frame = 0;

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
        // 'lighter' so overlapping motes add up into a brighter core rather
        // than the topmost one winning; that additive pile-up is the glow.
        ctx!.globalCompositeOperation = 'lighter';
        for (const mote of motes) paint(ctx!, advance(mote, width, height));
        ctx!.globalCompositeOperation = 'source-over';
    }

    function loop() {
        draw();
        frame = requestAnimationFrame(loop);
    }

    function stop() {
        if (!frame) return;
        cancelAnimationFrame(frame);
        frame = 0;
    }

    const observer = new IntersectionObserver(([entry]) => {
        if (entry.isIntersecting && !frame) frame = requestAnimationFrame(loop);
        else if (!entry.isIntersecting) stop();
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

/** Capped at 2: a 3x backing store costs 9x the fill for no visible gain here. */
function devicePixelRatio(): number {
    return Math.min(window.devicePixelRatio || 1, 2);
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

/** Advance one mote, wrapping it; up-and-out re-enters from the bottom at a fresh x. */
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
