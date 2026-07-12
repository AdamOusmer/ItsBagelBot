<script lang="ts">
  // Custom cursor: a tan dot tracking the pointer 1:1, plus a ring that lerps
  // behind it. On hover over an interactive element the ring MORPHS into that
  // element's box (rounded rect matching its bounds) — the cursor becomes the
  // button highlight — while the dot fades. Native pointer hidden (cursor:none)
  // on fine pointers. Coarse/touch keep the native cursor.
  //
  // Gated by the `customCursor` preference store: when the user turns it off the
  // effect tears down (listeners + the cursor:none class removed) and the native
  // pointer returns; turning it back on re-arms it live, with no reload.
  import { customCursor } from '../lib/cursor';

  let dot = $state<HTMLDivElement>();
  let ring = $state<HTMLDivElement>();

  $effect(() => {
    if (!$customCursor) return;

    const fine = window.matchMedia('(min-width: 901px) and (pointer: fine)');
    if (!fine.matches) return;

    // Bound in the same render flush that enables the cursor; effects run after
    // the DOM is patched, so both nodes exist here. Guard anyway for the types,
    // then alias to non-null locals the nested tick closure can use directly.
    if (!dot || !ring) return;
    const dotEl = dot;
    const ringEl = ring;

    let mx = innerWidth / 2,
      my = innerHeight / 2,
      // ring geometry (top-left x/y, w, h, radius)
      x = mx,
      y = my,
      w = 36,
      h = 36,
      r = 18,
      target: HTMLElement | null = null,
      raf = 0,
      lastMove = 0;

    document.documentElement.classList.add('bb-cursor-on');
    const lerp = (a: number, b: number, t: number) => a + (b - a) * t;
    const can = () => fine.matches && !document.hidden;
    const start = () => {
      if (!raf && can()) raf = requestAnimationFrame(tick);
    };

    function tick(now: number) {
      const hovering = !!target && target.isConnected;
      // dot: follows 1:1, fades out while morphed onto a target
      dotEl.style.transform = `translate(${mx}px, ${my}px)`;
      dotEl.style.opacity = hovering ? '0' : '1';

      let tx: number, ty: number, tw: number, th: number, tr: number;
      if (hovering) {
        const b = target!.getBoundingClientRect();
        const pad = 6;
        tw = b.width + pad * 2;
        th = b.height + pad * 2;
        tx = b.left - pad;
        ty = b.top - pad;
        tr = Math.min((parseFloat(getComputedStyle(target!).borderRadius) || 8) + pad, th / 2);
      } else {
        tw = 36;
        th = 36;
        tx = mx - 18;
        ty = my - 18;
        tr = 18;
      }
      const e = hovering ? 0.25 : 0.16;
      x = lerp(x, tx, e);
      y = lerp(y, ty, e);
      w = lerp(w, tw, e);
      h = lerp(h, th, e);
      r = lerp(r, tr, e);
      ringEl.style.transform = `translate(${x.toFixed(1)}px, ${y.toFixed(1)}px)`;
      ringEl.style.width = `${w.toFixed(1)}px`;
      ringEl.style.height = `${h.toFixed(1)}px`;
      ringEl.style.borderRadius = `${r.toFixed(1)}px`;
      ringEl.classList.toggle('morph', hovering);

      const settled = !hovering && Math.abs(x - tx) < 0.3 && Math.abs(y - ty) < 0.3;
      if (!can() || (settled && now - lastMove > 500)) {
        raf = 0;
        return;
      }
      raf = requestAnimationFrame(tick);
    }

    const onMove = (e: PointerEvent) => {
      if (e.pointerType === 'touch') return;
      mx = e.clientX;
      my = e.clientY;
      lastMove = performance.now();
      start();
    };
    const sel = 'a, button, .search, [data-cursor]';
    const over = (e: PointerEvent) => {
      const el = (e.target as Element | null)?.closest<HTMLElement>(sel);
      if (el) {
        target = el;
        start();
      }
    };
    const out = (e: PointerEvent) => {
      const el = (e.target as Element | null)?.closest<HTMLElement>(sel);
      if (el && el === target) {
        target = null;
        start();
      }
    };

    window.addEventListener('pointermove', onMove, { passive: true });
    document.addEventListener('pointerover', over, { passive: true });
    document.addEventListener('pointerout', out, { passive: true });
    document.addEventListener('visibilitychange', () => document.hidden || start());

    return () => {
      if (raf) cancelAnimationFrame(raf);
      document.documentElement.classList.remove('bb-cursor-on');
      window.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerover', over);
      document.removeEventListener('pointerout', out);
    };
  });
</script>

{#if $customCursor}
  <div class="cursor" bind:this={dot}></div>
  <div class="cursor-ring" bind:this={ring}></div>
{/if}

<style>
  .cursor {
    position: fixed;
    width: 12px;
    height: 12px;
    left: -6px;
    top: -6px;
    border-radius: 50%;
    background: var(--bb-tan, #c9a87c);
    pointer-events: none;
    z-index: 9999;
    will-change: transform, opacity;
    mix-blend-mode: difference;
    transition: opacity 200ms;
  }
  .cursor-ring {
    position: fixed;
    left: 0;
    top: 0;
    width: 36px;
    height: 36px;
    border: 2px solid var(--bb-tan, #c9a87c);
    border-radius: 8px;
    pointer-events: none;
    z-index: 9998;
    opacity: 0.5;
    transition: opacity 250ms, background 250ms, border-color 250ms;
    will-change: transform, width, height, border-radius;
  }
  /* morphed onto a button: the cursor stamps its own tan color onto the
     element, filling it with a tan wash and a tan border. */
  :global(.cursor-ring.morph) {
    opacity: 1;
    background: rgba(201, 168, 124, 0.22);
    border-color: var(--bb-tan, #c9a87c);
  }
  :global(html.bb-cursor-on),
  :global(html.bb-cursor-on *) {
    cursor: none !important;
  }
  @media (max-width: 900px), (pointer: coarse) {
    .cursor,
    .cursor-ring {
      display: none;
    }
  }
</style>
