// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { BUILTIN_NAMES } from '../../../../kit/lib/catalog/builtin-commands';
import type { CommandView } from '@bagel/kit';
import { logger } from '@bagel/kit/server/logger';
import { deleteCommand, listCommands, upsertCommand, upsertModule } from './commands-store';

export type BulkOp = 'enable' | 'disable' | 'delete';

export interface BulkResult {
  name: string;
  ok: boolean;
}

export const BULK_MAX = 100;

export const isBulkOp = (v: unknown): v is BulkOp => v === 'enable' || v === 'disable' || v === 'delete';

export function isBulkRequest(op: unknown, names: string[]): op is BulkOp {
  return isBulkOp(op) && names.length > 0 && names.length <= BULK_MAX;
}

async function toggleOne(uid: string, name: string, isActive: boolean, custom: Map<string, CommandView>) {
  if (BUILTIN_NAMES.has(name)) {
    await upsertModule(uid, name, isActive);
    return;
  }
  const c = custom.get(name);
  if (!c) throw new Error('unknown command');
  await upsertCommand(uid, {
    name: c.name,
    aliases: c.aliases ?? [],
    response: c.response,
    isActive,
    streamOnlineOnly: c.stream_online_only === true,
    perm: c.perm ?? 'everyone',
    cooldown: c.cooldown ?? 0,
    allowedUserId: c.allowed_user_id ?? '',
    bumpCounter: c.bump_counter ?? ''
  });
}

async function removeOne(uid: string, name: string, custom: Map<string, CommandView>) {
  if (BUILTIN_NAMES.has(name) || !custom.has(name)) throw new Error('not deletable');
  await deleteCommand(uid, name);
}

export async function runBulk(uid: string, op: BulkOp, names: string[]): Promise<BulkResult[]> {
  const custom = new Map((await listCommands(uid)).filter((c) => !c.builtin).map((c) => [c.name, c]));
  const results: BulkResult[] = [];
  for (const name of names) {
    try {
      if (op === 'delete') await removeOne(uid, name, custom);
      else await toggleOne(uid, name, op === 'enable', custom);
      results.push({ name, ok: true });
    } catch (e) {
      logger.error({ err: e }, `[commands] bulk ${op} failed`);
      results.push({ name, ok: false });
    }
  }
  return results;
}
