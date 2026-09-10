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

import { observeDecode, type DecodeOptions } from '../lib/decode';
import { observeReveal, type RevealOptions } from '../lib/reveal';
import { mountMagnetic, type MagneticOptions } from '../lib/magnetic';

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

/**
 * Decode every `[data-decode]` in this subtree as it scrolls into view.
 *
 * ```svelte
 * <h1 data-decode="Pricing" use:decode>Pricing</h1>
 * ```
 *
 * Same shape as `reveal` above and the same reason for existing: the marketing
 * site scans the document once per navigation, and a Svelte surface that wants
 * one decoding title should not have to.
 *
 * Disposing matters more here than for reveal: the engine writes `textContent`
 * on a frame loop, so a subtree removed mid-scramble would otherwise leave a
 * tick running against a detached node until its own clock expires.
 */
export const decode: Action<HTMLElement, DecodeOptions | undefined> = (node, options) => {
  const dispose = observeDecode(node, options);
  return { destroy: dispose };
};

/**
 * Magnetic hover: the element eases toward the pointer while it is over it.
 *
 * ```svelte
 * <button use:magnetic={{ strength: 0.4 }}>Connect</button>
 * ```
 *
 * The engine is ../lib/magnetic.ts, shared with the static surfaces'
 * `data-magnetic` spelling. This lived in web/kit/lib/actions.ts and was
 * therefore console-only; kit now re-exports this one, so its call sites did
 * not change and there is still exactly one implementation.
 *
 * Like `reveal` and `decode` above, options are read once at mount: the
 * engine caps and eases from them on every frame, and a mid-life change would
 * have to tear the listeners down and re-attach to take effect, which no
 * caller wants and an `update` that silently ignored its argument would hide.
 */
export const magnetic: Action<HTMLElement, MagneticOptions | undefined> = (node, options) => {
  const dispose = mountMagnetic(node, options);
  return { destroy: dispose };
};
