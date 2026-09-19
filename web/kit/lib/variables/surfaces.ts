// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Which Variables a Surface (CONTEXT.md "Language: reply templates") offers.
// Custom commands are the only surface this phase covers; module reply
// surfaces are wired from the module catalog in phase 2
// (docs/specs/variables-catalog.md section 5, feat/dashboard-chips-from-kit).

import { VARIABLES } from './variables';
import { HINT_KEYS } from './hint-keys';
import type { VariableDef, VariableForm } from './types';

/** Every Variable id the custom-command surface offers, in VARIABLES order. */
export const CUSTOM_COMMAND_VARIABLES: readonly string[] = VARIABLES.map((v) => v.id);

const CUSTOM_COMMAND_SET = new Set(CUSTOM_COMMAND_VARIABLES);

/** The Variables one Surface offers, in VARIABLES order. Only 'custom' exists
 * this phase; the union grows as later phases wire in module replies. */
export function forSurface(surface: 'custom'): readonly VariableDef[] {
  void surface;
  return VARIABLES.filter((v) => CUSTOM_COMMAND_SET.has(v.id));
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
