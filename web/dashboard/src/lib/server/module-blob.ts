// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// A module with NO row is disabled: `?? true` would silently turn it on for every broadcaster.
import { listModules, upsertModule } from './commands-store';

export type ModuleBlob<T> = { enabled: boolean; configs: T };

export async function readModuleBlob<T>(userId: string, modId: string): Promise<ModuleBlob<T>> {
  const rows = await listModules(userId);
  const row = rows.find((r) => r.name === modId);
  return { enabled: row ? row.is_enabled : false, configs: (row?.configs ?? {}) as T };
}

/** Raw blob write: the upsert replaces the whole value, and re-parsing drops unmodelled keys. */
export async function setModuleEnabled(userId: string, modId: string, enabled: boolean): Promise<void> {
  const { configs } = await readModuleBlob<Record<string, unknown>>(userId, modId);
  await upsertModule(userId, modId, enabled, configs);
}
