// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The five variables a rehearsal surface offers. Not "the first five of a
// longer list": five is the whole offer.
//
// Framework-free and sitting next to ./rehearsal.ts on purpose: the console's
// response editor (ResponseEditor.svelte) and the marketing command builder
// (web/marketing/src/components/builder/CommandBuilder.astro) render two
// different catalogs -- different token spellings, different sample values,
// different copy -- and the only thing they have to agree on is WHICH of them
// a first-time reader sees. That agreement is this file, and it is one list of
// five strings rather than two lists of forty entries.
//
// WHY FIVE, AND WHY NOTHING ELSE ON THE SURFACE (decision, 2026-09-10).
// Three shapes have now been tried on these two surfaces:
//   1. The whole catalog as chips -- 40 on the console, 40+ on the marketing
//      builder. The handful a first command is built out of sat in the middle
//      of a wall, so the wall taught nothing.
//   2. Eight chips plus a ghost "More variables" button that expanded the rest
//      in place. Better, but the button is an invitation to open the wall
//      again, and it re-introduced shape 1 one click in.
//   3. THIS: five chips, and no button, no expand-in-place, no hidden list
//      anywhere on the rehearsal surface. The rest of the catalog is real and
//      documented and will be reachable somewhere else; it is not this
//      surface's job. Do not re-add a toggle here -- that is shape 2, and it
//      was rejected by name.
// The cap is enforced by a test on the length of the array below, so growing
// it past five fails the suite rather than quietly widening the offer.
//
// WHY THESE FIVE, IN THIS ORDER. Who ran it, what they typed, where, a dice
// roll, a number that goes up: `user`, `args`, `channel`, `random`, then
// `count` when the catalog carries a counter and `uptime` when it does not.
// `count` and not `counter` because the console replaces the `{counter:…}`
// chip with the counter PICKER (which creates one in place) and would
// otherwise show the same variable twice. Both catalogs curated here do carry
// a `count` example, so `uptime` is the documented fallback rather than a
// sixth entry -- it is what a surface without counters shows in that slot.
//
// WHY HEADS AND NOT WHOLE TOKENS. A token that takes a payload is spelled with
// a DIFFERENT example payload on each surface -- the console's counter chip
// inserts `{count:deaths}` and the marketing catalog's says `{count:falls}` --
// so matching on the literal would silently drop the counter from one of the
// two lists the day somebody renamed an example. The head is the part before
// the first `:`, which is where ./tmpl.ts splits a token from its payload, so
// it means "this is the counter variable" rather than "this is one example of
// it".
//
// The dot is NOT a split point, and that is load-bearing. `{user}` and
// `{user.login}` are two different variables (display name vs lowercase
// login), as are `{random}`, `{random.chatter}` and `{random.emote}`; a head
// that stopped at the dot would pull five extra entries into a list whose
// whole purpose is to be five long.

/**
 * The head of a token: `{count:deaths}` -> `count`, `{user}` -> `user`,
 * `{user.login}` -> `user.login` (a different variable, see above). Anything
 * that is not a single `{…}` token returns ''.
 */
export function tokenHead(token: string): string {
  const inner = /^\{([^}]*)\}$/.exec(token.trim())?.[1];
  if (!inner) return '';
  return inner.split(':', 1)[0].trim().toLowerCase();
}

/**
 * The five heads a rehearsal surface offers, in preference order. A surface
 * whose catalog has no counter falls back to `uptime` for the fifth slot,
 * which is why six heads are spelled out for five slots and why the cap below
 * is what the test asserts.
 *
 * Order here is preference, NOT render order: each surface renders the picked
 * entries in its own catalog order, so the two never have to agree on
 * anything but membership.
 */
export const COMMON_TOKEN_HEADS: readonly string[] = ['user', 'args', 'channel', 'random', 'count'];

/** The fifth slot when a catalog carries no counter example. */
export const FALLBACK_TOKEN_HEAD = 'uptime';

/** How many chips a rehearsal surface offers. Five, and nothing else. */
export const COMMON_TOKEN_LIMIT = 5;

const HEADS = new Set([...COMMON_TOKEN_HEADS, FALLBACK_TOKEN_HEAD]);

/** Is this catalog entry one of the five a rehearsal surface can offer? */
export function isCommonToken(token: string): boolean {
  return HEADS.has(tokenHead(token));
}

/**
 * The common entries of one surface's catalog, in that catalog's own order,
 * AT MOST ONE PER HEAD and AT MOST FIVE IN TOTAL.
 *
 * Two rules the callers should not have to write themselves:
 *
 * De-dupe. A catalog is free to ship two examples of the same variable -- the
 * marketing builder lists both `{random}` and `{random:1-6}`, which are one
 * variable with and without a range -- and a plain filter would spend two of
 * the five slots on one variable. First one wins, because a catalog orders its
 * own entries plainest-first.
 *
 * The `uptime` fallback. It is only picked when the catalog carries no
 * counter, so a surface never shows six and never shows four when it has a
 * fifth to give.
 */
export function pickCommonTokens<T>(items: readonly T[], tokenOf: (item: T) => string): T[] {
  const heads = items.some((item) => tokenHead(tokenOf(item)) === 'count')
    ? COMMON_TOKEN_HEADS
    : [...COMMON_TOKEN_HEADS.filter((head) => head !== 'count'), FALLBACK_TOKEN_HEAD];
  const wanted = new Set(heads);
  const seen = new Set<string>();
  const picked: T[] = [];
  for (const item of items) {
    const head = tokenHead(tokenOf(item));
    if (!wanted.has(head) || seen.has(head)) continue;
    seen.add(head);
    picked.push(item);
    if (picked.length === COMMON_TOKEN_LIMIT) break;
  }
  return picked;
}
