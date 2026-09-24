// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export { magnetic } from '@bagel/ui/svelte/actions';

export { countUp } from '@bagel/ui/lib/count-up';

export async function initLenis(): Promise<() => void> {
  if (typeof window === 'undefined') return () => {};
  const { createSmoothScroll } = await import('@bagel/ui/lib/lenis');
  return createSmoothScroll()?.destroy ?? (() => {});
}
