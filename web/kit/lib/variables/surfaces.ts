// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Which Variables a Surface (CONTEXT.md "Language: reply templates") offers.
// Custom commands are the only surface this phase covers; module reply
// surfaces are wired from the module catalog in phase 2
// (docs/specs/variables-catalog.md section 5, feat/dashboard-chips-from-kit).

import { VARIABLES } from './variables';
import type { VariableDef } from './types';

/** Every Variable id the custom-command surface offers, in VARIABLES order. */
export const CUSTOM_COMMAND_VARIABLES: readonly string[] = VARIABLES.map((v) => v.id);

const CUSTOM_COMMAND_SET = new Set(CUSTOM_COMMAND_VARIABLES);

/** The Variables one Surface offers, in VARIABLES order. Only 'custom' exists
 * this phase; the union grows as later phases wire in module replies. */
export function forSurface(surface: 'custom'): readonly VariableDef[] {
  void surface;
  return VARIABLES.filter((v) => CUSTOM_COMMAND_SET.has(v.id));
}
