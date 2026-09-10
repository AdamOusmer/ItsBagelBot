// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Svelte `use:` actions over the framework-free engines in ../lib.
//
// This file is an ADAPTER, which is why it may import svelte where ../lib may
// not (ui/scripts/assert-framework-free.mjs enforces exactly that split). An
// action is the Svelte-shaped way to say "wire this DOM node and unwire it when
// it goes away"; the engines already speak `mount -> dispose`, so each action
// here is the two-line translation and nothing else.

import type { Action } from 'svelte/action';

import { observeReveal, type RevealOptions } from '../lib/reveal';

/**
 * Reveal every `[data-reveal]` in this subtree as it scrolls into view.
 *
 * ```svelte
 * <section use:reveal>
 *   <h2 data-reveal>…</h2>
 *   <p data-reveal style="--reveal-i: 1">…</p>
 * </section>
 * ```
 *
 * The node itself counts if it carries `data-reveal`, so `use:reveal` on the
 * element and on its wrapper both do the obvious thing.
 *
 * The marketing site does NOT use this — it scans the whole document once per
 * ClientRouter navigation, because on that site practically every section is a
 * reveal and one observer for the page is cheaper than one per section. This
 * exists for console pages, where reveals are the exception and a page-wide
 * scan would be an observer that finds nothing on most routes.
 *
 * The options object is read once, at mount. Re-running the observer because a
 * threshold changed mid-life is not a thing any caller wants, and an `update`
 * that silently ignored its argument would be worse than not having one.
 */
export const reveal: Action<HTMLElement, RevealOptions | undefined> = (node, options) => {
  const dispose = observeReveal(node, options);
  return { destroy: dispose };
};
