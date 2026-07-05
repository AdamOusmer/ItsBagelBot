<script lang="ts">
  import { onMount } from 'svelte';

  let canvas: HTMLCanvasElement;

  onMount(() => {
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
    if (reduceMotion.matches) return;

    const context = canvas.getContext('2d');
    if (!context) return;
    const ctx = context;

    type Mote = { x: number; y: number; r: number; vx: number; vy: number; alpha: number; warm: boolean };
    let width = 0;
    let height = 0;
    let dpr = Math.min(window.devicePixelRatio || 1, 2);
    let motes: Mote[] = [];
    let frame = 0;
    let visible = true;

    function build() {
      width = canvas.clientWidth;
      height = canvas.clientHeight;
      if (!width || !height) return;
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(height * dpr);
      const count = width < 700 ? 40 : 70;
      motes = Array.from({ length: count }, () => ({
        x: Math.random() * width,
        y: Math.random() * height,
        r: 0.6 + Math.random() * 2,
        vy: -(0.05 + Math.random() * 0.2),
        vx: (Math.random() - 0.5) * 0.1,
        alpha: 0.12 + Math.random() * 0.45,
        warm: Math.random() < 0.7
      }));
    }

    function draw() {
      if (!width || !height) build();
      if (!width || !height) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, width, height);
      ctx.globalCompositeOperation = 'lighter';

      for (const mote of motes) {
        mote.y += mote.vy;
        mote.x += mote.vx;
        if (mote.y < -10) { mote.y = height + 10; mote.x = Math.random() * width; }
        if (mote.x < -10) mote.x = width + 10;
        else if (mote.x > width + 10) mote.x = -10;
        const color = mote.warm ? '201, 168, 124' : '82, 183, 136';
        ctx.beginPath();
        ctx.arc(mote.x, mote.y, mote.r, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(${color}, ${mote.alpha.toFixed(3)})`;
        ctx.fill();
      }

      ctx.globalCompositeOperation = 'source-over';
    }

    function loop() {
      draw();
      frame = requestAnimationFrame(loop);
    }

    const observer = new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting;
      cancelAnimationFrame(frame);
      frame = visible ? requestAnimationFrame(loop) : 0;
    }, { rootMargin: '150px' });

    const resize = () => {
      dpr = Math.min(window.devicePixelRatio || 1, 2);
      build();
    };

    build();
    observer.observe(canvas);
    window.addEventListener('resize', resize, { passive: true });

    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener('resize', resize);
    };
  });
</script>

<canvas bind:this={canvas} class="light-field" aria-hidden="true"></canvas>

<style>
  .light-field {
    position: absolute;
    z-index: -1;
    inset: 0;
    display: block;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }

  @media (prefers-reduced-motion: reduce) {
    .light-field { display: none; }
  }
</style>
