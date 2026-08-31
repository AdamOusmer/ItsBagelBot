// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Per-stream Overview counters: messages seen, commands answered, and mod
// actions taken during the CURRENT stream, not the channel's lifetime totals.
//
// The underlying counters are lifetime-monotonic (loyalty's counter store,
// the same one public-stats.ts reads under the reserved bot-scope id — see
// that file's header for the shape; this file uses the real broadcaster id
// instead, since these are per-channel). A per-stream number is therefore
// always `current - baseline`, where baseline is the lifetime value the Go
// projector snapshotted into Valkey the moment this stream went live (see
// internal/projection/valkey.go's SetStreamCounterBaseline, written from
// app/projector/projector.go's HandleStreamEvent on the live false->true
// transition, via app/projector/loyalty.go's read of the same counter store
// this file reads).
//
// Two honesty rules drive this file:
//
//   1. No baseline yet (the channel has not gone live since this shipped, or
//      Valkey is unreachable) -> ok:false. Falling back to the lifetime total
//      would silently relabel "since forever" as "this stream," which is
//      worse than showing nothing.
//   2. A counter can go backwards relative to its baseline (service redeploy,
//      a manual counter.set). Clamp the delta at 0 rather than surface a
//      negative count — same precedent as public-stats.ts's perSecond clamp.
import Redis from 'iovalkey';
import { rpc } from '@bagel/shared/server/nats';
import { getServerConfig, hasServerConfig } from '@bagel/shared/server/config';
import { CircuitBreaker, withTimeout } from '@bagel/shared/server/resilience';
import {
  VALKEY_TLS_DATA_PORT,
  VALKEY_TLS_SENTINEL_PORT,
  valkeyEndpoint,
  valkeySentinelNAT,
  valkeyTLSOptions
} from '@bagel/shared/server/valkey-connection';
import { SUB } from './services';
import {
  allRead,
  degradedStreamCounters,
  type StreamCounters
} from '$lib/overview-live';

const SETTINGS_PREFIX = 'settings:';

// Field names must match internal/projection/valkey.go's
// streamCtrMessagesField / streamCtrAnsweredField / streamCtrModActionField
// exactly — this file and the Go projector read/write the same hash.
const FIELD_MESSAGES = 'streamctr:messages';
const FIELD_ANSWERED = 'streamctr:answered';
const FIELD_MOD_ACTIONS = 'streamctr:mod_actions';

// Loyalty counter names. messages_processed already has a real writer
// (sesame's bot_stats.go flushes it per channel every ~30s), and so do
// commands_answered and mod_actions, published from the same flush. All three
// are reserved system names (see SystemCounter in
// internal/domain/event/data/loyalty_events.go). A counter loyalty has not
// created yet answers found:false, which this file (like public-stats.ts's
// counterValue) treats as an honest 0, not an error.
const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_ANSWERED = 'commands_answered';
const COUNTER_MOD_ACTIONS = 'mod_actions';

const RPC_TIMEOUT_MS = 2000;
const OP_TIMEOUT_MS = 200;

// Own client + own breaker, deliberately not shared with valkey-master.ts's
// session-revocation client or valkey-store.ts's node-local read pool — see
// valkey-master.ts's header for why each critical-path dependency gets its
// own connection rather than a shared one.
const breaker = new CircuitBreaker({ name: 'valkey-stream-counters', failureThreshold: 3, resetMs: 5_000 });

const FAIL_FAST = {
  enableOfflineQueue: false,
  maxRetriesPerRequest: 1,
  connectTimeout: 1000,
  retryStrategy: (times: number) => Math.min(times * 200, 2000)
} as const;

type ValkeyConfig = NonNullable<ReturnType<typeof getServerConfig>['valkey']>;
type TLSOptions = ReturnType<typeof valkeyTLSOptions>;

function sentinelOptions(cfg: ValkeyConfig, tls: TLSOptions) {
  return {
    sentinels: [valkeyEndpoint(cfg.sentinelAddr as string, Boolean(tls), VALKEY_TLS_SENTINEL_PORT)],
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

// The baseline is written and read back close together (a dashboard opened
// right as a stream starts can race the write against a lagging replica), so
// this pins to the Sentinel-elected master — same reasoning as the Go store's
// GetStreamCounterBaseline (internal/projection/valkey.go), which pins to
// pkg/valkey/routing.go's Primary() for the identical race. Construction
// mirrors valkey-master.ts's masterClient() (sentinel vs. direct, same
// FAIL_FAST posture); duplicated rather than imported because that file is a
// dedicated write client for session-revocation and, per its own header,
// each critical-path dependency gets its own client + breaker.
function getMaster(): Redis | null {
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

interface Baseline {
  /** False when streamctr:messages is absent — mirrors the Go store's known check. */
  known: boolean;
  messages: number;
  answered: number;
  modActions: number;
}

const MISS_BASELINE: Baseline = { known: false, messages: 0, answered: 0, modActions: 0 };

/**
 * Read the go-live counter snapshot for one channel. known=false on a cold
 * key (channel has never gone live since this shipped) or any Valkey
 * failure — both degrade the same way, to "not measured yet."
 */
async function readBaseline(uid: string): Promise<Baseline> {
  const c = getMaster();
  if (!c) return MISS_BASELINE;
  try {
    const [messages, answered, modActions] = await breaker.run(() =>
      withTimeout(
        c.hmget(SETTINGS_PREFIX + uid, FIELD_MESSAGES, FIELD_ANSWERED, FIELD_MOD_ACTIONS),
        OP_TIMEOUT_MS,
        'valkey-stream-counters'
      )
    );
    if (messages === null) return MISS_BASELINE;
    return {
      known: true,
      messages: Number(messages) || 0,
      answered: Number(answered) || 0,
      modActions: Number(modActions) || 0
    };
  } catch {
    return MISS_BASELINE;
  }
}

interface CounterWire {
  counter?: { value?: number };
  found?: boolean;
}

/**
 * Read one channel-scope counter's current lifetime value via loyalty's
 * counter.get, the same RPC shape public-stats.ts uses under the bot-scope
 * id. Returns null only on an RPC failure (timeout, no responders) — a
 * counter loyalty has never created answers found:false, which reads as an
 * honest 0 (see this file's header).
 */
async function counterValue(uid: string, name: string): Promise<number | null> {
  try {
    const reply = await rpc<CounterWire>(`${SUB.loyalty}.counter.get`, { user_id: uid, name }, RPC_TIMEOUT_MS);
    const raw = reply.counter?.value;
    return Number.isFinite(raw) ? Number(raw) : 0;
  } catch {
    return null;
  }
}

function clampDelta(current: number, baseline: number): number {
  // Counters are monotonic; clamp so a counter reset (service redeploy,
  // manual counter.set) reads as 0 rather than a negative per-stream count —
  // same precedent as public-stats.ts's perSecond clamp.
  return Math.max(current - baseline, 0);
}

/**
 * The Overview's three per-stream counters for one channel. Never rejects —
 * any failure (no baseline yet, Valkey down, loyalty unreachable) resolves
 * to degradedStreamCounters() rather than inventing a number.
 */
export async function streamCounters(uid: string): Promise<StreamCounters> {
  const baseline = await readBaseline(uid);
  if (!baseline.known) return degradedStreamCounters();

  // Annotated as an array, not left as the tuple Promise.all infers: a type
  // predicate narrows an array cleanly but a tuple only partially, which
  // leaves the destructure below still nullable.
  const totals: (number | null)[] = await Promise.all([
    counterValue(uid, COUNTER_MESSAGES),
    counterValue(uid, COUNTER_ANSWERED),
    counterValue(uid, COUNTER_MOD_ACTIONS)
  ]);
  if (!allRead(totals)) return degradedStreamCounters();

  const [messages, answered, modActions] = totals;
  return {
    messages: clampDelta(messages, baseline.messages),
    answered: clampDelta(answered, baseline.answered),
    modActions: clampDelta(modActions, baseline.modActions),
    ok: true
  };
}
