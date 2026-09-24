// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { MODULE_CATALOG } from '@bagel/kit/catalog';
import { listModules, type ModuleView } from './commands-store';

const GATED_BUILTINS = ['followage', 'accountage', 'uptime', 'title', 'game'] as const;

export function flagsFromRows(rows: readonly ModuleView[]): Record<string, boolean> {
  const byName = new Map(rows.map((m) => [m.name, m]));
  const flags: Record<string, boolean> = {};

  for (const m of MODULE_CATALOG) {
    if (m.toggleable === false) continue;
    const row = byName.get(m.id);
    flags[m.id] = row ? row.is_enabled : false;
  }

  for (const id of GATED_BUILTINS) {
    const row = byName.get(id);
    flags[id] = row ? row.is_enabled : true;
  }

  return flags;
}

export const DEFAULT_MODULE_FLAGS: Record<string, boolean> = flagsFromRows([]);

export const DEMO_MODULE_FLAGS: Record<string, boolean> = Object.fromEntries(
  Object.keys(DEFAULT_MODULE_FLAGS).map((id) => [id, true])
);

export async function moduleFlags(userId: string): Promise<Record<string, boolean>> {
  return flagsFromRows(await listModules(userId));
}
