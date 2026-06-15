<script lang="ts">
  // design.google-style cursor: idle it is a small tan dot tracking the pointer;
  // on hover over an interactive target it morphs (lerps geometry) into a rounded
  // box matching that element's bounding rect — the cursor *becomes* the button
  // highlight. Fine-pointer + non-reduced-motion only; hidden on touch.
  import { onMount } from 'svelte';

  let blob: HTMLDivElement;

  onMount(() => {
    const fine = window.matchMedia('(min-width: 901px) and (pointer: fine)');
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');
    if (!fine.matches || reduce.matches) return;

    const DOT = 12;
    const PAD = 6; // grow slightly past the element edges
    let px = innerWidth / 2,
      py = innerHeight / 2; // raw pointer
    let target: HTMLElement | null = null;
    let raf = 0,
      lastMove = 0;

    // animated geometry (x/y = top-left)
    let x = px,
      y = py,
      w = DOT,
      h = DOT,
      r = DOT / 2;

    const lerp = (a: number, b: number, t: number) => a + (b - a) * t;

    function frame(now: number) {
      let tx: number, ty: number, tw: number, th: number, tr: number;
      if (target && target.isConnected) {
        const b = target.getBoundingClientRect();
        tw = b.width + PAD * 2;
        th = b.height + PAD * 2;
        tx = b.left - PAD;
        ty = b.top - PAD;
        const cssR = parseFloat(getComputedStyle(target).borderRadius) || 8;
        tr = Math.min(cssR + PAD, th / 2);
      } else {
        tw = DOT;
        th = DOT;
        tx = px - DOT / 2;
        ty = py - DOT / 2;
        tr = DOT / 2;
      }
      const e = 0.2;
      x = lerp(x, tx, e);
      y = lerp(y, ty, e);
      w = lerp(w, tw, e);
      h = lerp(h, th, e);
      r = lerp(r, tr, e);
      blob.style.transform = `translate(${x.toFixed(1)}px, ${y.toFixed(1)}px)`;
      blob.style.width = `${w.toFixed(1)}px`;
      blob.style.height = `${h.toFixed(1)}px`;
      blob.style.borderRadius = `${r.toFixed(1)}px`;

      const settled =
        !target &&
        Math.abs(x - tx) < 0.3 &&
        Math.abs(y - ty) < 0.3 &&
        Math.abs(w - tw) < 0.3 &&
        now - lastMove > 400;
      if (document.hidden || settled) {
        raf = 0;
        return;
      }
      raf = requestAnimationFrame(frame);
    }
    const kick = () => {
      if (!raf && !document.hidden) raf = requestAnimationFrame(frame);
    };

    const onMove = (e: PointerEvent) => {
      if (e.pointerType === 'touch') return;
      px = e.clientX;
      py = e.clientY;
      lastMove = performance.now();
      kick();
    };
    const onOver = (e: PointerEvent) => {
      const el = (e.target as Element | null)?.closest<HTMLElement>(
        'a, button, [data-cursor]'
      );
      if (el) {
        target = el;
        blob.classList.add('on');
        kick();
      }
    };
    const onOut = (e: PointerEvent) => {
      const el = (e.target as Element | null)?.closest<HTMLElement>('a, button, [data-cursor]');
      if (el && el === target) {
        target = null;
        blob.classList.remove('on');
        kick();
      }
    };

    window.addEventListener('pointermove', onMove, { passive: true });
    document.addEventListener('pointerover', onOver, { passive: true });
    document.addEventListener('pointerout', onOut, { passive: true });
    document.addEventListener('visibilitychange', () => !document.hidden && kick());
    kick();

    return () => {
      if (raf) cancelAnimationFrame(raf);
      window.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerover', onOver);
      document.removeEventListener('pointerout', onOut);
    };
  });
</script>

<div class="bb-cursor" bind:this={blob}></div>

<style>
  .bb-cursor {
    position: fixed;
    left: 0;
    top: 0;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--bb-tan, #c9a87c);
    pointer-events: none;
    z-index: 9999;
    will-change: transform, width, height, border-radius;
    mix-blend-mode: difference;
  }
  /* morphed onto an element: drop blend, become a soft accent highlight ring.
     `.on` is toggled from JS, so scope the base class and globalize the state. */
  .bb-cursor:global(.on) {
    mix-blend-mode: normal;
    background: var(--ui-accent-soft, rgba(82, 183, 136, 0.12));
    box-shadow:
      0 0 0 1px var(--ui-accent-light, #40916c),
      0 0 24px rgba(82, 183, 136, 0.25);
  }
  @media (max-width: 900px) {
    .bb-cursor {
      display: none;
    }
  }
</style>
