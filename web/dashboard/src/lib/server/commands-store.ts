// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import newrelic from 'newrelic';
import { rpc } from '@bagel/kit/server/nats';
import { POLICY } from '@bagel/kit/server/cache-keys';
import * as valkey from '@bagel/kit/server/valkey-store';
import type { CommandView, Perm } from '@bagel/kit';
import { SUB, fabric, invalidate } from './services';

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
}

export async function listModules(userId: string): Promise<ModuleView[]> {
  return readProjected<ModuleView>('modules', userId, async (id) => {
    const v = await valkey.getModules(id);
    return { projected: v.projected, rows: v.modules };
  });
}

async function replaceProjected(kind: section, userId: string, rows: unknown[]): Promise<boolean> {
  try {
    await rpc(`${SUB.projector}.${kind}.replace`, { user_id: userId, [kind]: rows }, 2000);
    return true;
  } catch (err) {
    newrelic.noticeError(err instanceof Error ? err : new Error(String(err)), {
      component: 'projector-replace',
      kind,
      userId
    });
    return false;
  }
}

export function replaceProjectedModules(userId: string, modules: ModuleView[]): Promise<boolean> {
  return replaceProjected('modules', userId, modules);
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
  await rpc(`${SUB.modules}.upsert`, {
    user_id: userId,
    name,
    is_enabled: isEnabled,
    configs: configs && Object.keys(configs as object).length ? configs : undefined
  });
  try {
    const current = await listModules(userId);
    const upserted: ModuleView = { name, is_enabled: isEnabled, configs: configs ?? {} };
    let merged = false;
    const modules = current.map((v) => {
      if (v.name === name) {
        merged = true;
        return upserted;
      }
      return v;
    });
    if (!merged) modules.push(upserted);

    const synced = await replaceProjected('modules', userId, modules);
    commitOptimistic(cacheKey('modules', userId), modules, synced);
    return { modules };
  } catch {
    invalidate(cacheKey('modules', userId));
    return { modules: [] };
  }
}

export interface ModulePatch {
  userId: string;
  name: string;
  isEnabled: boolean;
  partial: Record<string, string>;
  expectedRev: number;
}

export async function patchModule(p: ModulePatch): Promise<{ rev: number; conflict: boolean }> {
  const reply = await rpc<{ rev?: number; conflict?: boolean }>(`${SUB.modules}.patch`, {
    user_id: p.userId,
    name: p.name,
    is_enabled: p.isEnabled,
    configs: p.partial,
    expected_rev: p.expectedRev
  });
  if (reply.conflict) return { rev: reply.rev ?? p.expectedRev, conflict: true };
  invalidate(cacheKey('modules', p.userId));
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
  allowedUserId: string;
  bumpCounter: string;
}

export async function upsertCommand(
  userId: string,
  cmd: CommandInput,
  originalName?: string
): Promise<{ commands: CommandView[] }> {
  await rpc(`${SUB.commands}.upsert`, {
    user_id: userId,
    name: cmd.name,
    aliases: cmd.aliases,
    response: cmd.response,
    is_active: cmd.isActive,
    stream_online_only: cmd.streamOnlineOnly,
    perm: cmd.perm,
    cooldown: cmd.cooldown,
    allowed_user_id: cmd.allowedUserId,
    bump_counter: cmd.bumpCounter,
    original_name: originalName ?? ''
  });
  try {
    const current = await listCommands(userId);
    let commands = current;
    if (originalName && originalName !== cmd.name) {
      commands = commands.filter((c) => c.name !== originalName);
    }
    const upserted: CommandView = {
      name: cmd.name,
      aliases: cmd.aliases,
      response: cmd.response,
      is_active: cmd.isActive,
      stream_online_only: cmd.streamOnlineOnly,
      perm: cmd.perm,
      cooldown: cmd.cooldown,
      allowed_user_id: cmd.allowedUserId,
      bump_counter: cmd.bumpCounter,
      uses: current.find((c) => c.name === (originalName ?? cmd.name))?.uses
    };
    let merged = false;
    commands = commands.map((v) => {
      if (v.name === cmd.name) {
        merged = true;
        return upserted;
      }
      return v;
    });
    if (!merged) commands.push(upserted);

    const synced = await replaceProjected('commands', userId, commands);
    commitOptimistic(cacheKey('commands', userId), commands, synced);
    return { commands };
  } catch {
    invalidate(cacheKey('commands', userId));
    return { commands: [] };
  }
}

export async function deleteCommand(
  userId: string,
  name: string
): Promise<{ commands: CommandView[] }> {
  await rpc(`${SUB.commands}.delete`, { user_id: userId, name });
  try {
    const current = await listCommands(userId);
    const commands = current.filter((c) => c.name !== name);
    const synced = await replaceProjected('commands', userId, commands);
    commitOptimistic(cacheKey('commands', userId), commands, synced);
    return { commands };
  } catch {
    invalidate(cacheKey('commands', userId));
    return { commands: [] };
  }
}
