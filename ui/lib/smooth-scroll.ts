// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type Lenis from 'lenis';

type LenisWindow = Window & { __lenis?: Lenis };

const waiting = new Set<(lenis: Lenis) => void>();

export async function initLenis(): Promise<() => void> {
  if (typeof window === 'undefined') return () => {};
  const { createSmoothScroll } = await import('./lenis');
  const smooth = createSmoothScroll();
  if (smooth) for (const fn of [...waiting]) fn(smooth.lenis);
  return smooth?.destroy ?? (() => {});
}

export async function resetSmoothScroll(): Promise<void> {
  const { getSmoothScroll } = await import('./lenis');
  getSmoothScroll()?.scrollTo(0, { immediate: true });
}

export async function syncSmoothScroll(): Promise<void> {
  const { getSmoothScroll } = await import('./lenis');
  getSmoothScroll()?.scrollTo(window.scrollY, { immediate: true });
}

export function onSmoothScroll(fn: (lenis: Lenis) => void): () => void {
  const existing = typeof window === 'undefined' ? undefined : (window as LenisWindow).__lenis;
  if (existing) {
    fn(existing);
    return () => {};
  }
  const once = (lenis: Lenis) => {
    waiting.delete(once);
    fn(lenis);
  };
  waiting.add(once);
  return () => void waiting.delete(once);
}
