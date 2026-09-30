// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import newrelic from 'newrelic';
import { rpc } from '@bagel/kit/server/nats';
import { POLICY } from '@bagel/kit/server/cache-keys';
import * as valkey from '@bagel/kit/server/valkey-store';
import type { CommandView, Perm } from '@bagel/kit';
import { SUB, fabric, invalidate } from './services';
import { schedulePurgeChannel } from './edge-purge';

const READ_TIMEOUT_MS = 2000;

const UNSYNCED_TTL_MS = 5_000;

type section = 'commands' | 'modules';

function cacheKey(kind: section, userId: string): string {
  return `${kind}:${userId}`;
}

async function readProjected<T>(
  kind: section,
  userId: string,
  valkeyRead: (userId: string) => Promise<{ projected: boolean; rows: T[] }>
): Promise<T[]> {
  return fabric.readKey(cacheKey(kind, userId), POLICY.projected, async () => {
    const v = await valkeyRead(userId);
    if (v.projected) return v.rows;
    const r = await rpc<Record<string, T[]>>(`${SUB.projector}.${kind}.get`, { user_id: userId }, READ_TIMEOUT_MS);
    return r[kind] ?? [];
  });
}

export async function listCommands(userId: string): Promise<CommandView[]> {
  return readProjected<CommandView>('commands', userId, async (id) => {
    const v = await valkey.getCommands(id);
    return { projected: v.projected, rows: v.commands };
  });
}

export interface ModuleView {
  name: string;
  is_enabled: boolean;
  configs?: unknown;
  revision?: number;
  account_created_at?: number;
}

export async function listModules(userId: string): Promise<ModuleView[]> {
  return readProjected<ModuleView>('modules', userId, async (id) => {
    const v = await valkey.getModules(id);
    return { projected: v.projected, rows: v.modules };
  });
}

// Editors read the owning service so a conflict reload cannot reuse a stale projection.
export async function listModulesForEditing(userId: string): Promise<ModuleView[]> {
  const reply = await rpc<{ modules?: ModuleView[] }>(`${SUB.modules}.list`, { user_id: userId }, READ_TIMEOUT_MS);
  return reply.modules ?? [];
}

async function replaceProjected<R>(kind: section, userId: string, rows: unknown[]): Promise<{ reply: R } | null> {
  try {
    return { reply: await rpc<R>(`${SUB.projector}.${kind}.replace`, { user_id: userId, [kind]: rows }, 2000) };
  } catch (err) {
    newrelic.noticeError(err instanceof Error ? err : new Error(String(err)), {
      component: 'projector-replace',
      kind,
      userId
    });
    return null;
  }
}

export async function replaceProjectedModules(userId: string, modules: ModuleView[]): Promise<boolean> {
  const projected = await replaceProjected<{ modules: ModuleView[] }>('modules', userId, modules);
  schedulePurgeChannel(userId);
  // These are committed SQL rows; keep them briefly if projection is unavailable.
  commitOptimistic(cacheKey('modules', userId), projected ? projected.reply.modules : modules, projected !== null);
  return projected !== null;
}

function commitOptimistic<T>(key: string, value: T, synced: boolean): void {
  fabric.cache.set(key, value, synced ? POLICY.projected : UNSYNCED_TTL_MS);
}

export async function upsertModule(
  userId: string,
  name: string,
  isEnabled: boolean,
  configs?: unknown
): Promise<{ modules: ModuleView[] }> {
  const reply = await rpc<{ modules: ModuleView[] }>(`${SUB.modules}.upsert`, {
    user_id: userId,
    name,
    is_enabled: isEnabled,
    configs: configs && Object.keys(configs as object).length ? configs : undefined
  });
  await replaceProjectedModules(userId, reply.modules);
  return reply;
}

export interface ModulePatch {
  userId: string;
  name: string;
  isEnabled: boolean;
  partial: Record<string, string>;
  expectedRev: number;
}

export async function patchModule(p: ModulePatch): Promise<{ rev: number; conflict: boolean }> {
  const reply = await rpc<{ rev?: number; conflict?: boolean; modules: ModuleView[] }>(`${SUB.modules}.patch`, {
    user_id: p.userId,
    name: p.name,
    is_enabled: p.isEnabled,
    configs: p.partial,
    expected_rev: p.expectedRev
  });
  if (reply.conflict) {
    invalidate(cacheKey('modules', p.userId));
    return { rev: reply.rev ?? p.expectedRev, conflict: true };
  }
  await replaceProjectedModules(p.userId, reply.modules);
  return { rev: reply.rev ?? p.expectedRev + 1, conflict: false };
}

export interface CommandInput {
  name: string;
  aliases: string[];
  response: string;
  isActive: boolean;
  streamOnlineOnly: boolean;
  perm: Perm;
  cooldown: number;
  userCooldown: number;
  allowedUserId: string;
  bumpCounter: string;
  restoreUses?: number;
}

function commandFields(cmd: CommandInput) {
  return {
    name: cmd.name,
    aliases: cmd.aliases,
    response: cmd.response,
    is_active: cmd.isActive,
    stream_online_only: cmd.streamOnlineOnly,
    perm: cmd.perm,
    cooldown: cmd.cooldown,
    user_cooldown: cmd.userCooldown,
    allowed_user_id: cmd.allowedUserId,
    bump_counter: cmd.bumpCounter
  } satisfies CommandView;
}

function upsertRequest(userId: string, cmd: CommandInput, originalName: string | undefined, canRestore: boolean) {
  return {
    user_id: userId,
    ...commandFields(cmd),
    original_name: originalName ?? '',
    restore_uses: canRestore ? cmd.restoreUses : undefined
  };
}

function upsertedView(cmd: CommandInput, previous: CommandView | undefined, restored: boolean): CommandView {
  return {
    ...commandFields(cmd),
    uses: restored ? String(cmd.restoreUses) : previous?.uses,
    created_at: previous?.created_at
  };
}

function mergeUpserted(current: CommandView[], upserted: CommandView, originalName: string | undefined): CommandView[] {
  const renamedFrom = originalName && originalName !== upserted.name ? originalName : undefined;
  const kept = current.filter((c) => c.name !== renamedFrom);
  if (!kept.some((c) => c.name === upserted.name)) return [...kept, upserted];
  return kept.map((c) => (c.name === upserted.name ? upserted : c));
}

export async function upsertCommand(
  userId: string,
  cmd: CommandInput,
  originalName?: string
): Promise<{ commands: CommandView[]; restored: boolean }> {
  const canRestore = !originalName && (cmd.restoreUses ?? 0) > 0;
  const reply = await rpc<{ restored?: boolean }>(
    `${SUB.commands}.upsert`,
    upsertRequest(userId, cmd, originalName, canRestore)
  );
  const restored = canRestore && reply?.restored === true;
  schedulePurgeChannel(userId);
  try {
    const current = await listCommands(userId);
    const previous = current.find((c) => c.name === (originalName ?? cmd.name));
    const commands = mergeUpserted(current, upsertedView(cmd, previous, restored), originalName);
    const synced = (await replaceProjected('commands', userId, commands)) !== null;
    commitOptimistic(cacheKey('commands', userId), commands, synced);
    return { commands, restored };
  } catch {
    invalidate(cacheKey('commands', userId));
    return { commands: [], restored };
  }
}

export async function deleteCommand(
  userId: string,
  name: string
): Promise<{ commands: CommandView[] }> {
  await rpc(`${SUB.commands}.delete`, { user_id: userId, name });
  schedulePurgeChannel(userId);
  try {
    const current = await listCommands(userId);
    const commands = current.filter((c) => c.name !== name);
    const synced = (await replaceProjected('commands', userId, commands)) !== null;
    commitOptimistic(cacheKey('commands', userId), commands, synced);
    return { commands };
  } catch {
    invalidate(cacheKey('commands', userId));
    return { commands: [] };
  }
}
