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
import { listModules } from './commands-store';

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
