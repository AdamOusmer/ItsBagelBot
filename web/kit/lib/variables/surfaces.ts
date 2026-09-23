// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Which Variables a Surface (CONTEXT.md "Language: reply templates") offers.
// Custom commands are the only surface this phase covers; module reply
// surfaces are wired from the module catalog in phase 2
// (docs/specs/variables-catalog.md section 5, feat/dashboard-chips-from-kit).

import { VARIABLES } from './variables';
import { HINT_KEYS } from './hint-keys';
import { timerOwns } from '../engine/rehearsal';
import type { VariableDef, VariableForm } from './types';

/** Every Variable id the custom-command surface offers, in VARIABLES order. */
export const CUSTOM_COMMAND_VARIABLES: readonly string[] = VARIABLES.map((v) => v.id);

const CUSTOM_COMMAND_SET = new Set(CUSTOM_COMMAND_VARIABLES);

/** Every Variable a timer's message can resolve, in VARIABLES order —
 * whichever of a Variable's head or aliases engine/rehearsal.ts's timerOwns
 * claims. That function is Go's timerChain mirrored on this side (see its
 * own comment for which scopes a tick mounts and why); parity.test.ts checks
 * the id set this produces against app/twitch/sesame/engine/scope/testdata/
 * token_catalog.golden.json's surfaces.timer list, so the two cannot drift. */
const TIMER_VARIABLES: readonly string[] = VARIABLES.filter(
  (v) => timerOwns(v.head) || (v.aliases ?? []).some(timerOwns)
).map((v) => v.id);

const TIMER_SET = new Set(TIMER_VARIABLES);

/** The Variables one Surface offers, in VARIABLES order. 'custom' is every
 * Variable; 'timer' is the subset a timer's message can resolve — the union
 * grows further as later phases wire in module replies. */
export function forSurface(surface: 'custom' | 'timer'): readonly VariableDef[] {
  const set = surface === 'timer' ? TIMER_SET : CUSTOM_COMMAND_SET;
  return VARIABLES.filter((v) => set.has(v.id));
}

/** One dashboard insert chip: the literal text it inserts and the locale key
 * of its tooltip. */
export interface VariableChip {
  readonly token: string;
  readonly hintKey: string;
}

function chipsOf(v: VariableDef): VariableChip[] {
  const extra = (form: VariableForm): VariableChip[] =>
    form.chipHint ? [{ token: form.example, hintKey: `vars.${v.id}.${form.chipHint}` }] : [];
  return [{ token: v.forms[0].example, hintKey: HINT_KEYS[v.id] }, ...v.forms.slice(1).flatMap(extra)];
}

/** The chip strip one Surface shows, in VARIABLES order: the first form of
 * every Variable plus any form carrying chipHint. Shared by the dashboard
 * ResponseEditor and web/kit/lib/token-palettes.test.ts so the test checks
 * the strip the component renders rather than a re-implementation of it. */
export function chipsFor(surface: 'custom'): readonly VariableChip[] {
  return forSurface(surface).flatMap(chipsOf);
}
