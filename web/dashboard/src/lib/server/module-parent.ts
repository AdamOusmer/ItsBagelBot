// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { catalogChildren, type ModuleDef } from '@bagel/kit';
import { listModules, upsertModule } from './commands-store';

export async function parentIsEnabled(userId: string, parentId: string): Promise<boolean> {
  const rows = await listModules(userId);
  return rows.find((row) => row.name === parentId)?.is_enabled === true;
}

export async function disableChildren(userId: string, parentId: string): Promise<void> {
  const rows = await listModules(userId);
  const byName = new Map(rows.map((row) => [row.name, row]));
  for (const child of catalogChildren(parentId)) {
    const row = byName.get(child.id);
    if (!row?.is_enabled) continue;
    await upsertModule(userId, child.id, false, row.configs);
  }
}

export function isChildOf(def: ModuleDef | undefined, parentId: string): boolean {
  return !!def && def.parent === parentId;
}
