// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Moved from engine/common-tokens.ts, whose "five chips" cap is gone
// (docs/specs/variables-catalog.md phase 4: pinned chips replace the
// head-based cap, see VariableDef.pinned in ./types.ts and forSurface's
// consumers). tokenHead is still load-bearing on its own: variables/
// parity.test.ts uses it to compare a golden example's head against the
// manifest.

/**
 * The head of a token: `{count:deaths}` -> `count`, `{user}` -> `user`,
 * `{user.login}` -> `user.login` (a different variable: {user} and
 * {user.login} resolve to different fields, as do {random}, {random.chatter}
 * and {random.emote} — the dot is NOT a split point). Anything that is not a
 * single `{…}` token returns ''.
 */
export function tokenHead(token: string): string {
  const inner = /^\{([^}]*)\}$/.exec(token.trim())?.[1];
  if (!inner) return '';
  return inner.split(':', 1)[0].trim().toLowerCase();
}
