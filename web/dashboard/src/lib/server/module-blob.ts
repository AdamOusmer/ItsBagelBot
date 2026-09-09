// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Facade over listModules for the one question every per-module store asks:
// "is my module on, and what is in its config blob?".
//
// Ten stores each wrote the same four lines (list, find by name, default the
// enable flag, cast the configs), and the default was the part worth pinning:
// a module with NO row is disabled, not enabled -- a store that wrote
// `row?.is_enabled ?? true` would silently turn a module on for every
// broadcaster who has never opened its page.
//
// Kept as a read over listModules rather than a per-module RPC on purpose: the
// modules blob is one projected read the request has usually already paid for,
// and splitting it per module would multiply the round trips a page makes.
import { listModules, upsertModule } from './commands-store';

export type ModuleBlob<T> = { enabled: boolean; configs: T };

/**
 * Read one module's row. A missing row reads as disabled with an empty config,
 * which is what "never configured" means everywhere this is called.
 */
export async function readModuleBlob<T>(userId: string, modId: string): Promise<ModuleBlob<T>> {
  const rows = await listModules(userId);
  const row = rows.find((r) => r.name === modId);
  return { enabled: row ? row.is_enabled : false, configs: (row?.configs ?? {}) as T };
}

/**
 * Flip one module's enable flag, keeping its stored config exactly as it is.
 *
 * Lives next to readModuleBlob because the two are one pair: the upsert
 * replaces the WHOLE configs value, so a toggle that does not read first wipes
 * the module's settings. Four stores each wrote that read-then-write by hand
 * (channel points, quotes, timers, Govee), and they had drifted into
 * re-serializing their own parsed view on the way back out -- a toggle then
 * silently dropped anything the parser did not model (an unknown key, a reward
 * the validator refused). Writing back the raw blob makes a toggle a toggle.
 */
export async function setModuleEnabled(userId: string, modId: string, enabled: boolean): Promise<void> {
  const { configs } = await readModuleBlob<Record<string, unknown>>(userId, modId);
  await upsertModule(userId, modId, enabled, configs);
}
