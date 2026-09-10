// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Client behaviors shared by both consoles.
/* The magnetic hover engine lives in @bagel/ui/lib/magnetic.ts now, and the
 * `use:` wrapper in @bagel/ui/svelte/actions. It was already running on that
 * package's shared scheduler and its motion queries; what kept it here was
 * only that no static surface had asked for it, and the marketing site now
 * has. Re-exported rather than moved-and-renamed so the ~40
 * `import { magnetic } from '@bagel/kit'` call sites did not change, and so
 * there is exactly one implementation to tune.
 *
 * The two constants that were tuned here (EASE 0.2, SETTLE_PX 0.1) moved with
 * it, with their measurements. */
export { magnetic } from '@bagel/ui/svelte/actions';

/* Count-up lives in @bagel/ui/lib/count-up now: it is a plain
 * mount-and-teardown over textContent with no bot knowledge in it, and
 * StatTile -- its only caller -- moved to the library with it. Re-exported
 * rather than dropped so `import { countUp } from '@bagel/kit'` and
 * '@bagel/kit/actions' keep resolving; there is no second implementation. */
export { countUp } from '@bagel/ui/lib/count-up';

/**
 * Smooth scroll for the console shell. Returns a teardown; call it from
 * `onMount` and let the returned function be the effect's cleanup. No-op under
 * reduced motion, and a no-op teardown if a scroller was already running.
 *
 * The config, the `window.__lenis` publication and the rAF tick all live in
 * `@bagel/ui/lib/lenis` now — this is the console's thin, still-async wrapper.
 *
 * Still a dynamic import, and that is the point of the wrapper: it keeps
 * `lenis` (about 20 KB) out of the shell's initial chunk on a surface whose
 * first paint is a dashboard, not a scroll. A static import here would put the
 * library in the entry for every console route, including the ones that do not
 * scroll.
 */
export async function initLenis(): Promise<() => void> {
  if (typeof window === 'undefined') return () => {};
  const { createSmoothScroll } = await import('@bagel/ui/lib/lenis');
  return createSmoothScroll()?.destroy ?? (() => {});
}
