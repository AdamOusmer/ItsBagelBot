// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Must match internal/chatvolume's wire format.
import Redis from 'iovalkey';
import {
  VALKEY_TLS_DATA_PORT,
  VALKEY_TLS_SENTINEL_PORT,
  valkeyEndpoint,
  valkeySentinelNAT,
  valkeyTLSOptions
} from '@bagel/kit/server/valkey-connection';
import { getServerConfig, hasServerConfig, type ValkeyConfig } from '@bagel/kit/server/config';
import { CircuitBreaker, withTimeout } from '@bagel/kit/server/resilience';
import { logger } from '@bagel/kit/server/logger';
import { allRead, finiteOrNull, degradedChatVolume, type ChatVolume } from '../overview-live';

const RING_WIDTH = 60;
const OP_TIMEOUT_MS = 200;
const KEY_PREFIX = 'chatvol:';

const FAIL_FAST = {
  enableOfflineQueue: false,
  maxRetriesPerRequest: 1,
  connectTimeout: 1000,
  retryStrategy: (times: number) => Math.min(times * 200, 2000)
} as const;

type TLSOptions = ReturnType<typeof valkeyTLSOptions>;

function sentinelOptions(cfg: ValkeyConfig, tls: TLSOptions) {
  return {
    sentinels: [valkeyEndpoint(cfg.sentinelAddr as string, Boolean(tls), VALKEY_TLS_SENTINEL_PORT)],
      // `||`, not `??`: an unset VALKEY_MASTER_SET arrives as "" and must fall back.
    name: cfg.sentinelMaster || 'myprimary',
    password: cfg.password || undefined,
    sentinelPassword: cfg.password || undefined,
    tls,
    sentinelTLS: tls,
    enableTLSForSentinelMode: Boolean(tls),
    natMap: tls ? valkeySentinelNAT : undefined,
    sentinelRetryStrategy: (times: number) => Math.min(times * 200, 2000),
    ...FAIL_FAST
  };
}

function directOptions(cfg: ValkeyConfig, tls: TLSOptions) {
  const endpoint = valkeyEndpoint(cfg.addr, Boolean(tls), VALKEY_TLS_DATA_PORT);
  return { host: endpoint.host, port: endpoint.port, password: cfg.password || undefined, tls, ...FAIL_FAST };
}

let client: Redis | null = null;
let disabled = false;

function get(): Redis | null {
  if (disabled) return null;
  if (client) return client;
  const cfg = hasServerConfig() ? getServerConfig().valkey : undefined;
  if (!cfg) {
    disabled = true;
    return null;
  }
  const tls = valkeyTLSOptions(cfg);
  const c = new Redis(cfg.sentinelAddr ? sentinelOptions(cfg, tls) : directOptions(cfg, tls));
  c.on('error', () => {});
  client = c;
  return client;
}

const breaker = new CircuitBreaker({ name: 'valkey-chat-volume', failureThreshold: 3, resetMs: 5_000 });

function chatVolKey(uid: string): string {
  return KEY_PREFIX + uid;
}

function parseSlot(raw: string | undefined, wantDelta: number): { count: number; handled: boolean } {
  const empty = { count: 0, handled: false };
  const parts = raw?.split(':') ?? [];
  if (parts.length !== 3) return empty;

  const nums = [finiteOrNull(parts[0]), finiteOrNull(parts[1])];
  if (!allRead(nums)) return empty;

  const [delta, count] = nums;
  return delta === wantDelta ? { count, handled: parts[2] === '1' } : empty;
}

function slotName(epoch: number): string {
  return String(((epoch % RING_WIDTH) + RING_WIDTH) % RING_WIDTH);
}

function buildChatVolume(fields: Record<string, string>, nowEpochMin: number): ChatVolume {
  const anchor = Number(fields.a);
  const buckets: number[] = [];
  const commandTicks: number[] = [];
  for (let i = 0; i < RING_WIDTH; i++) {
    const target = nowEpochMin - (RING_WIDTH - 1) + i;
    const { count, handled } = parseSlot(fields[slotName(target)], target - anchor);
    buckets.push(count);
    if (handled) commandTicks.push(i);
  }
  return {
    buckets,
    commandTicks,
    now: buckets[RING_WIDTH - 1] ?? 0,
    peak: buckets.reduce((max, v) => Math.max(max, v), 0),
    ok: true
  };
}

export async function chatVolume(uid: string): Promise<ChatVolume> {
  const c = get();
  if (!c) return degradedChatVolume();
  try {
    const fields = await breaker.run(() =>
      withTimeout(c.hgetall(chatVolKey(uid)), OP_TIMEOUT_MS, 'valkey chat-volume read')
    );
    return buildChatVolume(fields, Math.floor(Date.now() / 60_000));
  } catch (err) {
    logger.warn({ err }, '[chat-volume] read failed, degrading');
    return degradedChatVolume();
  }
}
