// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { moduleDef, type ModuleState } from '@bagel/kit';

export type ModuleStateWire = Omit<ModuleState, 'def'> & { id: string };

export function toWire({ def, ...state }: ModuleState): ModuleStateWire {
  return { id: def.id, ...state };
}

export function hydrateModules(wire: readonly ModuleStateWire[]): ModuleState[] {
  return wire.flatMap(({ id, ...state }) => {
    const def = moduleDef(id);
    return def ? [{ def, ...state }] : [];
  });
}
