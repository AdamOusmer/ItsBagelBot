// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleState } from '@bagel/kit';

/** Refreshes can start before a save and finish after its acknowledgement. */
export function reconcileModuleToggles(
  incoming: ModuleState[],
  current: ModuleState[],
  pending: ReadonlyMap<string, boolean>
): ModuleState[] {
  const previous = new Map(current.map((row) => [row.def.id, row]));
  return incoming.map((row) => {
    const saved = previous.get(row.def.id);
    const base = saved && (saved.revision ?? 0) > (row.revision ?? 0)
      ? { ...row, enabled: saved.enabled, config: saved.config, revision: saved.revision }
      : row;
    const enabled = pending.get(row.def.id);
    return enabled === undefined ? base : { ...base, enabled };
  });
}
