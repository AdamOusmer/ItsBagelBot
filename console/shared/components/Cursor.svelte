<script lang="ts">
  // Custom cursor, matching the marketing site (web/): a solid tan dot tracking
  // the pointer 1:1 that scales up on interactive hover, plus a lagging ring
  // that lerps toward the pointer and dims on hover. The native pointer is
  // hidden (cursor:none) on fine pointers so only this shows. Touch/coarse
  // pointers and reduced-motion keep the native cursor.
  import { onMount } from 'svelte';

  let dot: HTMLDivElement;
  let ring: HTMLDivElement;

  onMount(() => {
    const fine = window.matchMedia('(min-width: 901px) and (pointer: fine)');
    if (!fine.matches) return;

    let mx = 0,
      my = 0,
      rx = 0,
      ry = 0,
      hovering = false,
      raf = 0,
      lastMove = 0;

    document.documentElement.classList.add('bb-cursor-on');

    const can = () => fine.matches && !document.hidden;
    const start = () => {
      if (!raf && can()) raf = requestAnimationFrame(tick);
    };

    function tick(now: number) {
      dot.style.transform = `translate(${mx}px, ${my}px) scale(${hovering ? 2.2 : 1})`;
      rx += (mx - rx) * 0.12;
      ry += (my - ry) * 0.12;
      ring.style.transform = `translate(${rx}px, ${ry}px)`;
      ring.style.opacity = hovering ? '0.08' : '0.5';
      const settled = Math.abs(mx - rx) < 0.2 && Math.abs(my - ry) < 0.2;
      if (!can() || (!hovering && settled && now - lastMove > 500)) {
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
    const over = (e: PointerEvent) => {
      if ((e.target as Element | null)?.closest('a, button, [data-cursor], input, label')) {
        hovering = true;
        start();
      }
    };
    const out = (e: PointerEvent) => {
      if ((e.target as Element | null)?.closest('a, button, [data-cursor], input, label')) {
        hovering = false;
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

<div class="cursor" bind:this={dot}></div>
<div class="cursor-ring" bind:this={ring}></div>

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
    will-change: transform;
    mix-blend-mode: difference;
  }
  .cursor-ring {
    position: fixed;
    width: 36px;
    height: 36px;
    left: -18px;
    top: -18px;
    border: 2px solid var(--bb-tan, #c9a87c);
    border-radius: 50%;
    pointer-events: none;
    z-index: 9998;
    opacity: 0.5;
    transition: opacity 300ms;
    will-change: transform, opacity;
  }
  /* hide the native pointer only when the custom cursor is active */
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
