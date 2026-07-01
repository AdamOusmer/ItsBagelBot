// Read-only Valkey view of the settings projection (tier 2 of the read path).
//
// The Go projector owns and writes the `settings:<user_id>` hash; console only
// reads it, exactly like the worker's projection.Store reads. One hash per user:
//
//   settings:<user_id>
//     status                  free | paid | vip
//     active                  0 | 1
//     banned                  0 | 1
//     live                    0 | 1
//     commands:projected      1            (present once commands were projected)
//     command:<name>          raw JSON (CommandView)
//     modules:projected       1
//     module:<name>:enabled   0 | 1
//     module:<name>:config    raw JSON
//
// A single HGETALL serves tier+active+ban+modules+commands. Every read is
// fault-isolated: a timeout or error returns a "miss" (known:false /
// projected:false) so the caller degrades to the projector RPC, never throwing
// into SSR. Valkey is optional — if no addr is configured the store is a no-op
// and the read path is purely RPC.
import Redis from 'iovalkey';
import type { CommandView } from '../types';
import { getServerConfig } from './config';
import { CircuitBreaker, withTimeout } from './resilience';

const SETTINGS_PREFIX = 'settings:';
const OP_TIMEOUT_MS = 200;

// One breaker for the whole Valkey read tier. Once it trips (Valkey down/slow),
// reads return the miss sentinel immediately and go straight to RPC instead of
// paying OP_TIMEOUT_MS on every request until resetMs elapses. This is the core
// fault-isolation lever: a Valkey outage costs the timeout a few times, not on
// every page render.
const breaker = new CircuitBreaker({ name: 'valkey', failureThreshold: 3, resetMs: 5_000 });

/**
 * Run a Valkey op under the breaker + a per-op timeout. Returns `fallback` (the
 * caller's miss sentinel) on a disabled store, an open circuit, a timeout, or
 * any error — never throws into SSR.
 */
async function op<T>(run: (c: Redis) => Promise<T>, fallback: T): Promise<T> {
  const c = get();
  if (!c) return fallback;
  try {
    return await breaker.run(() => withTimeout(run(c), OP_TIMEOUT_MS, 'valkey'));
  } catch {
    return fallback;
  }
}

function settingsKey(userId: string): string {
  return SETTINGS_PREFIX + userId;
}

let client: Redis | null = null;
let disabled = false;

/** Lazily build the node-local read client, or null when Valkey is unconfigured. */
function get(): Redis | null {
  if (disabled) return null;
  if (client) return client;
  const cfg = getServerConfig().valkey;
  if (!cfg) {
    disabled = true;
    return null;
  }
  const [host, portStr] = cfg.addr.split(':');
  client = new Redis({
    host: host || '127.0.0.1',
    port: portStr ? Number(portStr) : 6379,
    password: cfg.password || undefined,
    // No offline queue: while Valkey is unreachable, ops fail immediately and
    // readers fall through to RPC (op() returns the miss sentinel). Queueing
    // would buy nothing here — every op is already bounded by OP_TIMEOUT_MS —
    // and an extended outage would grow the queue without bound. The boot
    // warm() + readyz probe cover the brief cold-connect window.
    enableOfflineQueue: false,
    maxRetriesPerRequest: 1,
    connectTimeout: 1000,
    retryStrategy: (times) => Math.min(times * 200, 2000)
  });
  // Swallow connection errors; reads degrade to RPC and the client reconnects.
  client.on('error', () => {});
  return client;
}

/**
 * Pre-connect the read client at boot so the first request hits a warm pool
 * instead of paying the connect on the hot path. Best-effort and a no-op when
 * Valkey is unconfigured. Call once from the init hook.
 */
export function warm(): void {
  get();
}

/**
 * Probe the read pool, connecting it as a side effect (PING under the breaker +
 * timeout). Returns true when Valkey is reachable OR unconfigured (Valkey is an
 * optional read tier, never a readiness blocker — see the readyz handler).
 *
 * Wired into /readyz so every probe re-warms the pool: a rotated-in pod's pool
 * is hot within one probe interval of boot regardless of boot-time state, so the
 * first real request rarely pays a cold connect. Failure here is non-fatal: the
 * read path falls through to RPC.
 */
export async function ready(): Promise<boolean> {
  if (!getServerConfig().valkey) return true;
  return op(async (c) => {
    await c.ping();
    return true;
  }, true);
}

export interface ValkeyUser {
  status: string;
  active: boolean;
  banned: boolean;
  /** False when the user has never been projected (cold key) — escalate to RPC. */
  known: boolean;
}

export interface ProjectedModule {
  name: string;
  is_enabled: boolean;
  configs?: unknown;
}

const MISS_USER: ValkeyUser = { status: '', active: false, banned: false, known: false };

/** Read tier/active/ban for one user. known=false on cold key or any failure. */
export function getUser(userId: string): Promise<ValkeyUser> {
  return op(async (c) => {
    const res = await c.hmget(settingsKey(userId), 'status', 'active', 'banned');
    const status = res[0] ?? '';
    if (!status) return MISS_USER;
    return { status, active: res[1] === '1', banned: res[2] === '1', known: true };
  }, MISS_USER);
}

/** Read the projected command list. projected=false on cold key or any failure. */
export function getCommands(
  userId: string
): Promise<{ commands: CommandView[]; projected: boolean }> {
  return op(async (c) => {
    const fields = await c.hgetall(settingsKey(userId));
    let projected = fields['commands:projected'] === '1';
    const commands: CommandView[] = [];
    for (const [field, value] of Object.entries(fields)) {
      const name = field.startsWith('command:') ? field.slice('command:'.length) : '';
      if (!name) continue;
      try {
        commands.push(JSON.parse(value) as CommandView);
        projected = true;
      } catch {
        // Malformed entry — skip.
      }
    }
    return { commands, projected };
  }, { commands: [], projected: false });
}

/** Read the projected module list. projected=false on cold key or any failure. */
export function getModules(
  userId: string
): Promise<{ modules: ProjectedModule[]; projected: boolean }> {
  return op(async (c) => {
    const fields = await c.hgetall(settingsKey(userId));
    let projected = fields['modules:projected'] === '1';
    const byName = new Map<string, ProjectedModule>();
    for (const [field, value] of Object.entries(fields)) {
      const parsed = parseModuleField(field);
      if (!parsed) continue;
      const mod = byName.get(parsed.name) ?? { name: parsed.name, is_enabled: false };
      if (parsed.suffix === 'enabled') {
        mod.is_enabled = value === '1';
        projected = true;
      } else if (parsed.suffix === 'config') {
        mod.configs = safeJson(value);
        projected = true;
      }
      byName.set(parsed.name, mod);
    }
    return { modules: [...byName.values()], projected };
  }, { modules: [], projected: false });
}

function parseModuleField(field: string): { name: string; suffix: string } | null {
  const rest = field.startsWith('module:') ? field.slice('module:'.length) : '';
  if (!rest) return null;
  const idx = rest.lastIndexOf(':');
  if (idx < 0) return null;
  return { name: rest.slice(0, idx), suffix: rest.slice(idx + 1) };
}

function safeJson(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return undefined;
  }
}
