// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The manifest shape (docs/specs/variables-catalog.md section 4): one
// VariableDef per Variable (CONTEXT.md "Language: reply templates"), never
// per Token or per Form. A Variable can accept more than one Token (aliases)
// and can take more than one Form; the manifest keeps those as arrays on one
// entry rather than as separate entries, so a guide page, a chip and a parity
// test all walk the same list.

/** The 13 groupings a guide page or a chip palette sorts a Variable into. */
export type VariableCategory =
  | 'basics'
  | 'arguments'
  | 'counters'
  | 'dynamic'
  | 'utilities'
  | 'viewer'
  | 'channel'
  | 'chat'
  | 'emotes'
  | 'alerts'
  | 'rewards'
  | 'queue'
  | 'game-stats';

/** One shape a Variable's Token can take: the syntax a chip inserts, one
 * worked example of it, and the output the shared rehearsal (or, for the
 * external family, a plausible stand-in) substitutes for that example. */
export interface VariableForm {
  readonly syntax: string;
  readonly example: string;
  readonly output: string;
}

export interface VariableDef {
  /** 'followage'; the i18n key suffix under vars.<id> and the guide anchor.
   * Never a literal Go head, so a head with a '.' in it (user.login) or a
   * shared shape (positional) still gets a plain identifier. */
  readonly id: string;
  /** The lexer head exactly as ./engine/common-tokens.ts tokenHead() returns
   * it ('user.login', 'count', 'if'). 'positional' stands in for every {1},
   * {2:} … {30} span, which tokenHead() would otherwise return as the literal
   * digits. */
  readonly head: string;
  readonly category: VariableCategory;
  /** The canonical spelling is forms[0]; a guide page renders it as the chip. */
  readonly forms: readonly VariableForm[];
  /** Other heads scope.Message resolves to the same field ('target' for
   * touser, 'sender' for user; see messageFields in
   * app/twitch/sesame/engine/scope/message.go). */
  readonly aliases?: readonly string[];
  readonly legacy?: boolean;
  /** A module id (see lib/catalog/*.ts) the bot needs switched on before this
   * Variable answers; absent when nothing gates it. */
  readonly requires?: string;
}
