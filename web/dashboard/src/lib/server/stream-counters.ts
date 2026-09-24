// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import Redis from 'iovalkey';
import { rpc } from '@bagel/kit/server/nats';
import { getServerConfig, hasServerConfig } from '@bagel/kit/server/config';
import { CircuitBreaker, withTimeout } from '@bagel/kit/server/resilience';
import {
  VALKEY_TLS_DATA_PORT,
  VALKEY_TLS_SENTINEL_PORT,
  valkeyEndpoint,
  valkeySentinelNAT,
  valkeyTLSOptions
} from '@bagel/kit/server/valkey-connection';
import { SUB } from './services';
import {
  allRead,
  degradedStreamCounters,
  type StreamCounters
} from '$lib/overview-live';

const SETTINGS_PREFIX = 'settings:';

// Must match internal/projection/valkey.go's streamCtr*Field names.
const FIELD_MESSAGES = 'streamctr:messages';
const FIELD_ANSWERED = 'streamctr:answered';
const FIELD_MOD_ACTIONS = 'streamctr:mod_actions';

const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_ANSWERED = 'commands_answered';
const COUNTER_MOD_ACTIONS = 'mod_actions';

const RPC_TIMEOUT_MS = 2000;
const OP_TIMEOUT_MS = 200;

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
  known: boolean;
  messages: number;
  answered: number;
  modActions: number;
}

const MISS_BASELINE: Baseline = { known: false, messages: 0, answered: 0, modActions: 0 };

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
  return Math.max(current - baseline, 0);
}

export async function streamCounters(uid: string): Promise<StreamCounters> {
  const baseline = await readBaseline(uid);
  if (!baseline.known) return degradedStreamCounters();

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
