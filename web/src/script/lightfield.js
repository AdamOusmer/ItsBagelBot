/**
 * Standalone warm light-field for non-scrubbed surfaces (inner-page heroes).
 * Any `<canvas data-field>` gets the same drifting mote field the cinematic
 * scenes use, at a constant warmth. rAF gated by visibility; reduced-motion off.
 */

const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

function setupField(canvas) {
    if (canvas.dataset.fieldReady === "true") return;
    canvas.dataset.fieldReady = "true";
    if (reduceMotion.matches) return;

    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    let w = 0, h = 0, motes = [];
    let dpr = Math.min(window.devicePixelRatio || 1, 2);
    const warmth = parseFloat(canvas.dataset.warmth) || 0.7;

    function build() {
        w = canvas.clientWidth;
        h = canvas.clientHeight;
        if (!w || !h) return;
        canvas.width = Math.round(w * dpr);
        canvas.height = Math.round(h * dpr);
        const count = w < 700 ? 40 : 70;
        motes = Array.from({ length: count }, () => ({
            x: Math.random() * w,
            y: Math.random() * h,
            r: 0.6 + Math.random() * 2,
            vy: -(0.05 + Math.random() * 0.2),
            vx: (Math.random() - 0.5) * 0.1,
            a: 0.12 + Math.random() * 0.45,
            warm: Math.random(),
        }));
    }

    // renderMote advances one mote and paints it, wrapping it around the
    // viewport edges (up-and-out re-enters from the bottom at a fresh x).
    function renderMote(m) {
        m.y += m.vy;
        m.x += m.vx;
        if (m.y < -10) { m.y = h + 10; m.x = Math.random() * w; }
        if (m.x < -10) m.x = w + 10; else if (m.x > w + 10) m.x = -10;
        const col = m.warm < warmth ? "201, 168, 124" : "82, 183, 136";
        ctx.beginPath();
        ctx.arc(m.x, m.y, m.r, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(${col}, ${m.a.toFixed(3)})`;
        ctx.fill();
    }

    function draw() {
        if (!w || !h || !motes.length) build();
        if (!w || !h) return;
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        ctx.clearRect(0, 0, w, h);
        ctx.globalCompositeOperation = "lighter";
        for (const m of motes) renderMote(m);
        ctx.globalCompositeOperation = "source-over";
    }

    let rafId = 0;
    function loop() { draw(); rafId = requestAnimationFrame(loop); }

    const io = new IntersectionObserver(([e]) => {
        if (e.isIntersecting && !rafId) rafId = requestAnimationFrame(loop);
        else if (!e.isIntersecting && rafId) { cancelAnimationFrame(rafId); rafId = 0; }
    }, { rootMargin: "150px" });
    io.observe(canvas);

    window.addEventListener("resize", () => { dpr = Math.min(window.devicePixelRatio || 1, 2); build(); }, { passive: true });
    build();
}

function setup() {
    document.querySelectorAll("canvas[data-field]").forEach(setupField);
}

setup();
document.addEventListener("astro:page-load", setup);
