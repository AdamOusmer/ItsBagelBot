// Commands + modules store: the read path and the optimistic write pipeline.
//
// Reads are the fabric's hybrid path (L1 -> Valkey projection -> projector RPC).
// Writes go to the owning service (source of truth, write-behind batcher that
// flushes within ~2s), then the store makes the change visible immediately:
//
//   1. merge the new row into the cached list (optimistic, this replica);
//   2. push the merged list to the projector (`projector.*.replace`) so the
//      Valkey projection — read by other replicas AND the chat worker — is
//      correct now instead of after the event pipeline catches up;
//   3. write-through the merged list into L1.
//
// If the projector push fails, the optimistic entry is kept only for 5s
// (instead of the projected policy's long window): the UI stays snappy but
// re-reads Valkey/RPC almost immediately, so a diverged optimistic list can't
// survive for minutes. The event pipeline (data.*.changed -> projector ->
// cache-invalidation bus) reconciles everything shortly after either way.
import newrelic from 'newrelic';
import { rpc } from '@bagel/shared/server/nats';
import { POLICY } from '@bagel/shared/server/cache-keys';
import * as valkey from '@bagel/shared/server/valkey-store';
import type { CommandView, Perm } from '@bagel/shared';
import { SUB, fabric, invalidate } from './services';

const READ_TIMEOUT_MS = 2000;

// Optimistic entries that failed the projector push decay fast (see above).
const UNSYNCED_TTL_MS = 5_000;

export async function listCommands(userId: string): Promise<CommandView[]> {
  return fabric.readKey(`commands:${userId}`, POLICY.projected, async () => {
    const v = await valkey.getCommands(userId);
    if (v.projected) return v.commands;
    const r = await rpc<{ commands: CommandView[] }>(
      `${SUB.projector}.commands.get`,
      { user_id: userId },
      READ_TIMEOUT_MS
    );
    return r.commands ?? [];
  });
}

export interface ModuleView {
  name: string;
  is_enabled: boolean;
  configs?: unknown;
}

export async function listModules(userId: string): Promise<ModuleView[]> {
  return fabric.readKey(`modules:${userId}`, POLICY.projected, async () => {
    const v = await valkey.getModules(userId);
    if (v.projected) return v.modules;
    const r = await rpc<{ modules: ModuleView[] }>(
      `${SUB.projector}.modules.get`,
      { user_id: userId },
      READ_TIMEOUT_MS
    );
    return r.modules ?? [];
  });
}

// Best-effort projection push. Returns whether the projector confirmed it, so
// callers can decide how long to trust their optimistic cache entry. Failures
// are logged (they silently degraded freshness for minutes before) but never
// thrown — the change-event pipeline reconciles the projection regardless.
async function replaceProjectedCommands(userId: string, commands: CommandView[]): Promise<boolean> {
  try {
    await rpc(`${SUB.projector}.commands.replace`, { user_id: userId, commands }, 2000);
    return true;
  } catch (err) {
    newrelic.noticeError(err instanceof Error ? err : new Error(String(err)), {
      component: 'projector-replace',
      kind: 'commands',
      userId
    });
    return false;
  }
}

export async function replaceProjectedModules(userId: string, modules: ModuleView[]): Promise<boolean> {
  try {
    await rpc(`${SUB.projector}.modules.replace`, { user_id: userId, modules }, 2000);
    return true;
  } catch (err) {
    newrelic.noticeError(err instanceof Error ? err : new Error(String(err)), {
      component: 'projector-replace',
      kind: 'modules',
      userId
    });
    return false;
  }
}

// Commit an optimistically merged list: trusted for the full projected window
// when the projection push confirmed, only briefly when it did not.
function commitOptimistic<T>(key: string, value: T, synced: boolean): void {
  fabric.cache.set(key, value, synced ? POLICY.projected : UNSYNCED_TTL_MS);
}

// upsertModule writes one module's enabled flag + config to the modules service
// (source of truth, write-behind), then optimistically refreshes the projection
// and the local cache, mirroring upsertCommand.
export async function upsertModule(
  userId: string,
  name: string,
  isEnabled: boolean,
  configs?: unknown
): Promise<{ modules: ModuleView[]; error?: string }> {
  const r = await rpc<{ error?: string }>(`${SUB.modules}.upsert`, {
    user_id: userId,
    name,
    is_enabled: isEnabled,
    // Omit empty configs so the service stores nothing rather than "{}".
    configs: configs && Object.keys(configs as object).length ? configs : undefined
  });
  if (!r.error) {
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

    const synced = await replaceProjectedModules(userId, modules);
    commitOptimistic(`modules:${userId}`, modules, synced);
    return { modules, error: r.error };
  } else {
    invalidate(`modules:${userId}`);
    return { modules: await listModules(userId), error: r.error };
  }
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
}

// originalName, when set and different from cmd.name, renames the command: the
// commands service updates the existing row's name field in place instead of
// deleting the old command and recreating it under the new name.
export async function upsertCommand(
  userId: string,
  cmd: CommandInput,
  originalName?: string
): Promise<{ commands: CommandView[]; error?: string }> {
  const r = await rpc<{ error?: string }>(`${SUB.commands}.upsert`, {
    user_id: userId,
    name: cmd.name,
    aliases: cmd.aliases,
    response: cmd.response,
    is_active: cmd.isActive,
    stream_online_only: cmd.streamOnlineOnly,
    perm: cmd.perm,
    cooldown: cmd.cooldown,
    allowed_user_id: cmd.allowedUserId,
    original_name: originalName ?? ''
  });
  if (!r.error) {
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
      // Preserve the lifetime counter through the optimistic merge — edits
      // never change it and losing it here would flash 0 in the UI.
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

    const synced = await replaceProjectedCommands(userId, commands);
    commitOptimistic(`commands:${userId}`, commands, synced);
    return { commands, error: r.error };
  } else {
    invalidate(`commands:${userId}`);
    return { commands: await listCommands(userId), error: r.error };
  }
}

export async function deleteCommand(
  userId: string,
  name: string
): Promise<{ commands: CommandView[]; error?: string }> {
  const r = await rpc<{ error?: string }>(`${SUB.commands}.delete`, {
    user_id: userId,
    name
  });
  if (!r.error) {
    const current = await listCommands(userId);
    const commands = current.filter((c) => c.name !== name);
    const synced = await replaceProjectedCommands(userId, commands);
    commitOptimistic(`commands:${userId}`, commands, synced);
    return { commands, error: r.error };
  } else {
    invalidate(`commands:${userId}`);
    return { commands: await listCommands(userId), error: r.error };
  }
}
