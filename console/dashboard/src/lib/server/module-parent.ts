// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Cross-module enable coupling: a nested catalog child (gamble, duel) cannot
// stay on once its parent (loyalty) is off, and cannot be flipped on while
// the parent is off. The dashboard is the authoring half of that gate;
// sesame's ReadLoyaltyConfig is the runtime half, so a stale enabled row
// still cannot spend points against a currency that is not running.

import { catalogChildren, type ModuleDef } from '@bagel/shared';
import { listModules, upsertModule } from './commands-store';

export async function parentIsEnabled(userId: string, parentId: string): Promise<boolean> {
  const rows = await listModules(userId);
  return rows.find((row) => row.name === parentId)?.is_enabled === true;
}

// disableChildren turns off every nested module under parentId, preserving
// each child's stored config so a later re-enable does not wipe odds/replies.
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
