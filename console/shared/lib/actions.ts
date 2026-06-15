// Client behaviors shared by both consoles.
import type { Action } from 'svelte/action';

type MagneticOpts = { strength?: number; max?: number };

/**
 * design.google-style magnetic hover: while the pointer is over the element it
 * eases toward the cursor by `strength` of the offset from center, capped at
 * `max` px. Releases back to rest on leave. Respects reduced-motion and skips
 * coarse pointers. Use as `use:magnetic` on buttons/links.
 */
export const magnetic: Action<HTMLElement, MagneticOpts | undefined> = (node, opts) => {
  const fine = typeof window !== 'undefined' && window.matchMedia('(pointer: fine)').matches;
  const reduce = typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (!fine || reduce) return;

  const strength = opts?.strength ?? 0.3;
  const max = opts?.max ?? 14;
  let tx = 0,
    ty = 0,
    cx = 0,
    cy = 0,
    raf = 0;

  node.style.transition = 'transform 0.3s cubic-bezier(0.16,1,0.3,1)';
  node.style.willChange = 'transform';

  const cap = (v: number) => Math.max(-max, Math.min(max, v));

  const loop = () => {
    cx += (tx - cx) * 0.2;
    cy += (ty - cy) * 0.2;
    node.style.transform = `translate(${cx.toFixed(2)}px, ${cy.toFixed(2)}px)`;
    if (Math.abs(tx - cx) > 0.1 || Math.abs(ty - cy) > 0.1) raf = requestAnimationFrame(loop);
    else raf = 0;
  };
  const kick = () => {
    if (!raf) raf = requestAnimationFrame(loop);
  };

  const move = (e: PointerEvent) => {
    const r = node.getBoundingClientRect();
    tx = cap((e.clientX - (r.left + r.width / 2)) * strength);
    ty = cap((e.clientY - (r.top + r.height / 2)) * strength);
    node.style.transition = 'none';
    kick();
  };
  const leave = () => {
    tx = 0;
    ty = 0;
    node.style.transition = 'transform 0.4s cubic-bezier(0.16,1,0.3,1)';
    kick();
  };

  node.addEventListener('pointermove', move);
  node.addEventListener('pointerleave', leave);

  return {
    destroy() {
      if (raf) cancelAnimationFrame(raf);
      node.removeEventListener('pointermove', move);
      node.removeEventListener('pointerleave', leave);
    }
  };
};

/**
 * Smooth scroll via lenis, mirroring web/'s config. Returns a teardown. Call in
 * onMount; no-op for reduced-motion.
 */
export async function initLenis(): Promise<() => void> {
  if (typeof window === 'undefined') return () => {};
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return () => {};
  const { default: Lenis } = await import('lenis');
  const lenis = new Lenis({ lerp: 0.1, smoothWheel: true, syncTouch: false });
  // Exposed so navigation can keep lenis and the native scroll in sync (avoids a
  // janky jump on page change).
  (window as unknown as { __lenis?: unknown }).__lenis = lenis;
  let raf = 0;
  const tick = (t: number) => {
    lenis.raf(t);
    raf = requestAnimationFrame(tick);
  };
  raf = requestAnimationFrame(tick);
  return () => {
    cancelAnimationFrame(raf);
    delete (window as unknown as { __lenis?: unknown }).__lenis;
    lenis.destroy();
  };
}
