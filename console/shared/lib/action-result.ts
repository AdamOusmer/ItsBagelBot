// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The enhance-callback boundary, shared by both consoles.
//
// Eleven pages each re-declared the same four-line unwrap plus their own
// `ActionResult` type, and they had already drifted: one page treated a
// `failure` result as "no payload" and so showed the generic fallback instead
// of the server's message. One unwrap, one base shape.
//
// Named action-result.ts, not actions.ts: `lib/actions.ts` is the Svelte
// `use:` actions module (magnetic, countUp), an unrelated meaning of the word.
//
// The local type name was `ActionResult` in six of those pages, which shadows
// SvelteKit's own `ActionResult` (the thing being unwrapped). `ActionOk` names
// what the payload actually is: the server's reply body.

/** Base success/failure payload every form action returns. Pages widen it. */
export type ActionOk = { ok?: boolean; error?: string };

/**
 * Unwrap the data a form action returned. `failure` carries a payload just as
 * `success` does (that is how `fail(400, {...})` reaches the client), so both
 * are unwrapped; `redirect` and `error` carry none.
 */
export function actionPayload<T = ActionOk>(result: unknown): T | undefined {
  const r = result as { type?: string; data?: T } | null;
  return r?.type === 'success' || r?.type === 'failure' ? r.data : undefined;
}

/**
 * Factory for a page's `failed(payload, fallbackKey)`: binds the toast sink and
 * the translator once so call sites stay two arguments.
 *
 * A factory rather than a four-argument function because every page has exactly
 * one (toast, t) pair but many call sites; passing them at each site is what the
 * copies did, and it is what made them worth copying rather than importing.
 */
export function toastFailure(
  toast: (kind: 'err', text: string) => unknown,
  t: (key: string) => string
): (payload: ActionOk | undefined, fallbackKey: string) => void {
  return (payload, fallbackKey) => {
    toast('err', payload?.error ?? t(fallbackKey));
  };
}
