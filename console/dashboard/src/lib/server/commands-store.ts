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

// section names the projected collection (commands or modules) a store
// operation targets; it drives the cache key and the projector subject verbs so
// the read/replace paths stay identical for both.
type section = 'commands' | 'modules';

function cacheKey(kind: section, userId: string): string {
  return `${kind}:${userId}`;
}

// readProjected is the shared list read path (L1 -> Valkey projection ->
// projector RPC) for either collection. valkeyRead answers whether the Valkey
// projection is populated and, if so, its rows; a cold projection falls back to
// the projector's get RPC. pick names the field on the RPC reply.
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

// replaceProjected is the best-effort projection push for either collection.
// Returns whether the projector confirmed it, so callers can decide how long to
// trust their optimistic cache entry. Failures are logged (they silently
// degraded freshness for minutes before) but never thrown — the change-event
// pipeline reconciles the projection regardless.
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

// Commit an optimistically merged list: trusted for the full projected window
// when the projection push confirmed, only briefly when it did not.
function commitOptimistic<T>(key: string, value: T, synced: boolean): void {
  fabric.cache.set(key, value, synced ? POLICY.projected : UNSYNCED_TTL_MS);
}

// upsertModule writes one module's enabled flag + config to the modules service
// (source of truth, write-behind), then optimistically refreshes the projection
// and the local cache, mirroring upsertCommand.
//
// The write RPC throws (RpcError / timeout / no-responders) when the write
// itself fails — callers convert that into a `fail()` so the real reason reaches
// the toast. The projection/cache refresh AFTER a confirmed write is best-effort:
// a hiccup reading it back must never turn a landed write into a reported
// failure (that was the old bug — a slow projector made a successful toggle look
// broken and gave no feedback).
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
    // Omit empty configs so the service stores nothing rather than "{}".
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
    // Write landed but the read-back failed: drop the stale cache and let the
    // next load re-read. The operation still succeeded.
    invalidate(cacheKey('modules', userId));
    return { modules: [] };
  }
}

// patchModule merges a subset of config keys into one module under optimistic
// concurrency. `partial` carries only the keys to change (an explicit "" clears a
// key); `expectedRev` is the revision the client last read. The service reports a
// conflict when the stored revision has moved on, so the caller reloads and
// retries rather than clobbering a concurrent edit. Returns the new revision.
export async function patchModule(
  userId: string,
  name: string,
  isEnabled: boolean,
  partial: Record<string, string>,
  expectedRev: number
): Promise<{ rev: number; conflict: boolean }> {
  const reply = await rpc<{ rev?: number; conflict?: boolean }>(`${SUB.modules}.patch`, {
    user_id: userId,
    name,
    is_enabled: isEnabled,
    configs: partial,
    expected_rev: expectedRev
  });
  if (reply.conflict) return { rev: reply.rev ?? expectedRev, conflict: true };
  // Landed: drop the cached projection so the next read reflects the merged blob.
  invalidate(cacheKey('modules', userId));
  return { rev: reply.rev ?? expectedRev + 1, conflict: false };
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
): Promise<{ commands: CommandView[] }> {
  // Write to the source of truth. A thrown RpcError/timeout means the write
  // failed; let it propagate so the action reports the real reason as a fail().
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

    const synced = await replaceProjected('commands', userId, commands);
    commitOptimistic(cacheKey('commands', userId), commands, synced);
    return { commands };
  } catch {
    // Write landed but the read-back failed: the operation still succeeded.
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
