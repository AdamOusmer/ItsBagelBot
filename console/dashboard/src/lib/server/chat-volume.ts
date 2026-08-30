// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Read-only Valkey view of the Overview chat-volume chart: the per-channel,
// per-minute ring buffer sesame's Go observer writes
// (internal/chatvolume/chatvolume.go). That Go package's doc comment owns
// the storage decision record; this file only mirrors its wire format, since
// this TypeScript reader cannot import the Go package directly.
//
// Wire format — one HASH per channel, "chatvol:<uid>":
//   a          anchor epoch (unix-minutes) of the ring's first write.
//   "0".."59"  ring slots, keyed by epoch%ringWidth: "<delta>:<count>:<handled>",
//              where delta=epoch-anchor. A slot whose stored delta does not
//              match the delta the reader expects for that exact minute
//              belongs to a different lap around the ring (or was never
//              written) and reads as zero — see parseSlot.
//
// Own dedicated Sentinel/master-pinned client, not a share of valkey-store.ts's
// node-local pool or valkey-master.ts's session-revocation write client: the
// current minute's bucket is written on every chat message and read back on
// every dashboard load, so a node-local (replica) read would routinely show
// the just-elapsed minute as short or zero for the length of the replication
// window. Primary consistency needs its own client + breaker here, same
// per-dependency isolation reasoning as valkey-master.ts and rate-limit.ts's
// own separate write clients (a fault on one must not perturb the others).
import Redis from 'iovalkey';
import {
  VALKEY_TLS_DATA_PORT,
  VALKEY_TLS_SENTINEL_PORT,
  valkeyEndpoint,
  valkeySentinelNAT,
  valkeyTLSOptions
} from '@bagel/shared/server/valkey-connection';
import { getServerConfig, hasServerConfig, type ValkeyConfig } from '@bagel/shared/server/config';
import { CircuitBreaker, withTimeout } from '@bagel/shared/server/resilience';
import { logger } from '@bagel/shared/server/logger';
import { degradedChatVolume, type ChatVolume } from '../overview-live';

const RING_WIDTH = 60;
const OP_TIMEOUT_MS = 200;
const KEY_PREFIX = 'chatvol:';

// Fail fast, never queue: an unreachable primary must degrade this panel
// immediately (the breaker + op() below take over), not grow an unbounded
// command queue behind a dashboard read.
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
    // `||` not `??`: an empty VALKEY_MASTER_SET (unset in Doppler comes
    // through as "") must fall back to the sentinel's monitored name, not be
    // used verbatim — see valkey-master.ts's identical guard.
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
  // Swallow connection errors; reads degrade via op()'s breaker/timeout and
  // the client reconnects in the background.
  c.on('error', () => {});
  client = c;
  return client;
}

const breaker = new CircuitBreaker({ name: 'valkey-chat-volume', failureThreshold: 3, resetMs: 5_000 });

function chatVolKey(uid: string): string {
  return KEY_PREFIX + uid;
}

/**
 * One ring slot's value, or the zero reading when the slot was never written
 * or belongs to a different lap (see the wire-format doc above). Mirrors
 * internal/chatvolume's readSlot/parseSlotValue exactly.
 */
function parseSlot(raw: string | undefined, wantDelta: number): { count: number; handled: boolean } {
  if (!raw) return { count: 0, handled: false };
  const parts = raw.split(':');
  if (parts.length !== 3) return { count: 0, handled: false };
  const delta = Number(parts[0]);
  const count = Number(parts[1]);
  if (!Number.isFinite(delta) || !Number.isFinite(count) || delta !== wantDelta) {
    return { count: 0, handled: false };
  }
  return { count, handled: parts[2] === '1' };
}

function slotName(epoch: number): string {
  return String(((epoch % RING_WIDTH) + RING_WIDTH) % RING_WIDTH);
}

/**
 * Reconstructs the last RING_WIDTH minutes oldest-first from the raw hash.
 * A missing/malformed anchor parses to NaN, which never equals any real
 * delta below — so an empty or cold ring comes back all-zero without a
 * separate branch for it.
 */
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

/**
 * Read one channel's chat-volume ring for the Overview chart. Never throws:
 * an unconfigured store, a tripped breaker, a timeout or any other failure
 * all degrade to degradedChatVolume() so a Valkey blip never fails the page.
 */
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
