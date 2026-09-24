// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import Redis from 'iovalkey';
import type { CommandView } from '../types';
import { getServerConfig } from './config';
import { logger } from './logger';
import { CircuitBreaker, withTimeout } from './resilience';
import { VALKEY_TLS_DATA_PORT, valkeyEndpoint, valkeyTLSOptions } from './valkey-connection';

const SETTINGS_PREFIX = 'settings:';
const SESSION_REVOKED_PREFIX = 'session-revoked:';
const SESSION_REVOKED_ALL_PREFIX = 'session-revoked-all:';
const OP_TIMEOUT_MS = 200;

const breaker = new CircuitBreaker({ name: 'valkey', failureThreshold: 3, resetMs: 5_000 });

// Separate breaker: a tripped read-tier breaker must not disable the session-revocation check.
const revocationBreaker = new CircuitBreaker({
  name: 'valkey-session-revocation-read',
  failureThreshold: 3,
  resetMs: 5_000
});

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

export function sessionRevokedKey(sid: string): string {
  return SESSION_REVOKED_PREFIX + sid;
}

export function sessionRevokedAllKey(userId: string): string {
  return SESSION_REVOKED_ALL_PREFIX + userId;
}

let client: Redis | null = null;
let disabled = false;

function get(): Redis | null {
  if (disabled) return null;
  if (client) return client;
  const cfg = getServerConfig().valkey;
  if (!cfg) {
    disabled = true;
    return null;
  }
  const tls = valkeyTLSOptions(cfg);
  const endpoint = valkeyEndpoint(cfg.addr, Boolean(tls), VALKEY_TLS_DATA_PORT);
  client = new Redis({
    host: endpoint.host,
    port: endpoint.port,
    password: cfg.password || undefined,
    tls,
    enableOfflineQueue: false,
    maxRetriesPerRequest: 1,
    connectTimeout: 1000,
    retryStrategy: (times) => Math.min(times * 200, 2000)
  });
  client.on('error', () => {});
  return client;
}

export function warm(): void {
  get();
}

export async function ready(): Promise<boolean> {
  if (!getServerConfig().valkey) return true;
  return op(async (c) => {
    await c.ping();
    return true;
  }, true);
}

export interface RevocationKeys {
  sid?: string;
  userId: string;
}

export async function getRevocation({ sid, userId }: RevocationKeys): Promise<[string | null, string | null]> {
  const c = get();
  if (!c) return [null, null];
  try {
    // Without a sid, still check the revoke-all epoch: it exists to kill exactly those old cookies.
    const keys = sid ? [sessionRevokedKey(sid), sessionRevokedAllKey(userId)] : [sessionRevokedAllKey(userId)];
    const values = await revocationBreaker.run(() =>
      withTimeout(c.mget(...keys), OP_TIMEOUT_MS, 'valkey session-revocation read')
    );
    const [sidHit, allAt] = sid ? values : [null, values[0]];
    return [sidHit ?? null, allAt ?? null];
  } catch (err) {
    logger.warn({ err }, '[valkey-store] session-revocation read failed, failing open');
    return [null, null];
  }
}

export interface ValkeyUser {
  status: string;
  active: boolean;
  banned: boolean;
  known: boolean;
}

export interface ProjectedModule {
  name: string;
  is_enabled: boolean;
  configs?: unknown;
}

const MISS_USER: ValkeyUser = { status: '', active: false, banned: false, known: false };

export function getUser(userId: string): Promise<ValkeyUser> {
  return op(async (c) => {
    const res = await c.hmget(settingsKey(userId), 'status', 'active', 'banned');
    const status = res[0] ?? '';
    if (!status) return MISS_USER;
    return { status, active: res[1] === '1', banned: res[2] === '1', known: true };
  }, MISS_USER);
}

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
      } catch {}
    }
    return { commands, projected };
  }, { commands: [], projected: false });
}

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
