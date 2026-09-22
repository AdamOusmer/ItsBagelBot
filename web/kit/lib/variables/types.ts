// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The manifest shape (docs/specs/variables-catalog.md section 4): one
// VariableDef per Variable (CONTEXT.md "Language: reply templates"), never
// per Token or per Form. A Variable can accept more than one Token (aliases)
// and can take more than one Form; the manifest keeps those as arrays on one
// entry rather than as separate entries, so a guide page, a chip and a parity
// test all walk the same list.

/** The five groupings a guide page or a chip palette sorts a Variable into
 * (docs/specs/variables-catalog.md phase 4; replaces the 13-value
 * VariableCategory). Locale label at vars.group.<g>.label. */
export type VariableGroup = 'who' | 'typed' | 'stream' | 'fun' | 'data';

/** One shape a Variable's Token can take: the syntax a chip inserts, one
 * worked example of it, and the output the shared rehearsal (or, for the
 * external family, a plausible stand-in) substitutes for that example. */
export interface VariableForm {
  readonly syntax: string;
  readonly example: string;
  readonly output: string;
  /** Locale key suffix under vars.<id> when this form is offered as its own
   * dashboard chip beside the first form. Only positional's {2:} needs it: a
   * chip inserts literal text, and "the rest of the line" is a different
   * thing to insert than "word 1", so folding it into the first chip would
   * hide the form broadcasters reach for most. Everything else stays one
   * chip per Variable; extra forms are guide-only. */
  readonly chipHint?: string;
}

export interface VariableDef {
  /** 'followage'; the i18n key suffix under vars.<id> and the guide anchor.
   * Never a literal Go head, so a head with a '.' in it (user.login) or a
   * shared shape (positional) still gets a plain identifier. */
  readonly id: string;
  /** The lexer head exactly as ./head.ts tokenHead() returns it
   * ('user.login', 'count', 'if'). 'positional' stands in for every {1},
   * {2:} … {30} span, which tokenHead() would otherwise return as the literal
   * digits. */
  readonly head: string;
  readonly group: VariableGroup;
  /** The canonical spelling is forms[0]; a guide page renders it as the chip. */
  readonly forms: readonly VariableForm[];
  /** Other heads scope.Message resolves to the same field ('target' for
   * touser, 'sender' for user; see messageFields in
   * app/twitch/sesame/engine/scope/message.go). */
  readonly aliases?: readonly string[];
  readonly legacy?: boolean;
  /** A module id (see lib/catalog/*.ts) the bot needs switched on before this
   * Variable answers; null when nothing gates it. Every entry in
   * variables.ts sets this explicitly (never omits it) so a reader can tell
   * "checked, ungated" from "someone forgot to fill this in" — dashboard
   * gating on this field is phase 5's job, not this one's. */
  readonly requires?: string | null;
  /** True for the six Variables a capped rehearsal surface (the dashboard's
   * ResponseEditor, the marketing command builder) shows as chips:
   * user, args, touser, random, uptime, if. Replaces the old
   * engine/common-tokens.ts head allowlist (docs/specs/variables-catalog.md
   * phase 4) — this is a property OF the Variable now, not a separate list
   * two surfaces had to keep in sync by hand. See surfaces.test.ts for the
   * "at most six" rule this cap enforces. */
  readonly pinned?: boolean;
}
