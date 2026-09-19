// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// A typed lookup from a Variable id (docs/specs/variables-catalog.md D5, D8)
// to its vars.<id>.hint locale key. The command palette calls
// i18n.t(HINT_KEYS[v.id]), a computed key, which i18n/literal-keys.test.ts
// cannot see: its CALL regex only matches a literal string handed straight to
// t(...) (see that file's own header comment on dynamic lookups), and a
// computed key never is one. That gap is not a real hole here: every id's
// vars.<id>.hint is already asserted to exist in both locales by
// variables/parity.test.ts test D, so this map buys only what a locale test
// cannot: a typo in an id catches at compile time instead of showing up as a
// dangling key at runtime.
import { VARIABLES } from './variables';

export type VariableHintKey = `vars.${string}.hint`;

export const HINT_KEYS: Readonly<Record<string, VariableHintKey>> = Object.fromEntries(
  VARIABLES.map((v) => [v.id, `vars.${v.id}.hint` as const])
);
