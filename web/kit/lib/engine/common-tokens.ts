// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Which variables a rehearsal surface offers BEFORE you ask for the rest.
//
// Framework-free and sitting next to ./rehearsal.ts on purpose: the console's
// response editor (ResponseEditor.svelte) and the marketing command builder
// (web/marketing/src/components/builder/CommandBuilder.astro) render two
// different catalogs -- different token spellings, different sample values,
// different copy -- and the only thing they have to agree on is WHICH of them
// a first-time reader sees. That agreement is this file, and it is one list of
// eight strings rather than two lists of forty entries.
//
// WHY A SHORT LIST AT ALL. Both surfaces used to render the whole catalog as
// clickable chips: 40 on the console, 40+ on the marketing builder. Every one
// of them is real and every one of them is documented, and the effect of
// showing all of them at once is that the eight a new command actually needs
// are somewhere in the middle of a wall. The rest are not hidden -- they are
// one "More variables" click away, in the same block, and the toggle is a
// ghost Button on both surfaces so the two behave the same way.
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
// whole purpose is to be eight long.
//
// WHY THESE EIGHT. They are the tokens the documented starter commands are
// built out of: who ran it, where, what they typed, a number that goes up, a
// dice roll, and the three stream facts. `count` and not `counter` because the
// console replaces the `{counter:…}` chip with the counter PICKER (which
// creates one in place) and would otherwise show the same variable twice.

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
 * The eight heads every rehearsal surface offers before "More variables".
 * Order is NOT a render order: each surface keeps its own catalog order, so
 * the two never have to agree on anything but membership.
 */
export const COMMON_TOKEN_HEADS: readonly string[] = [
  'user',
  'channel',
  'args',
  'count',
  'random',
  'uptime',
  'game',
  'title',
];

const HEADS = new Set(COMMON_TOKEN_HEADS);

/** Is this catalog entry one of the eight shown before "More variables"? */
export function isCommonToken(token: string): boolean {
  return HEADS.has(tokenHead(token));
}

/**
 * The common entries of one surface's catalog, in that catalog's own order,
 * AT MOST ONE PER HEAD.
 *
 * The de-dupe is the reason this is a function and not a filter the callers
 * write themselves. A catalog is free to ship two examples of the same
 * variable -- the marketing builder lists both `{random}` and `{random:1-6}`,
 * which are one variable with and without a range -- and a plain filter would
 * put both of them in a list whose entire job is to be eight items long. First
 * one wins, because a catalog orders its own entries plainest-first.
 */
export function pickCommonTokens<T>(items: readonly T[], tokenOf: (item: T) => string): T[] {
  const seen = new Set<string>();
  const picked: T[] = [];
  for (const item of items) {
    const head = tokenHead(tokenOf(item));
    if (!HEADS.has(head) || seen.has(head)) continue;
    seen.add(head);
    picked.push(item);
  }
  return picked;
}
