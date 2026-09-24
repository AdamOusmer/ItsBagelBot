// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import newrelic from 'newrelic';
import { rpcCode, type CodedReply, type RpcCode } from './rpc-code';
import {
  connect,
  jwtAuthenticator,
  type Authenticator,
  type ConnectionOptions,
  type NatsConnection
} from '@nats-io/transport-node';
import { jetstream, jetstreamManager } from '@nats-io/jetstream';
import type {
  JetStreamClient,
  JetStreamManager,
  JetStreamManagerOptions
} from '@nats-io/jetstream';
import { requestLocalFirst, rpcSubjectsForNode } from './nats-rpc-locality';

function rpcSegment(subject: string): string {
  return subject
    .split('.')
    .map((t) => (/^\d+$/.test(t) ? '*' : t))
    .join('.');
}

type Role = 'rpc' | 'bus';

interface Pool {
  conn: NatsConnection | null;
  dialing: Promise<NatsConnection> | null;
}

const pools: Record<Role, Pool> = {
  rpc: { conn: null, dialing: null },
  bus: { conn: null, dialing: null }
};

const failbackEnabled = new WeakSet<NatsConnection>();
let failbackQueue: Promise<void> = Promise.resolve();

function positiveNumber(value: string | undefined, fallback: number): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

export async function localLeafReady(healthURL: string, timeoutMs: number): Promise<boolean> {
  try {
    const response = await fetch(healthURL, {
      signal: AbortSignal.timeout(timeoutMs),
      cache: 'no-store'
    });
    await response.body?.cancel();
    return response.ok;
  } catch {
    return false;
  }
}

export function enableLeafFailback(nc: NatsConnection): void {
  const nodeName = process.env.NODE_NAME;
  if (!nodeName || failbackEnabled.has(nc)) return;
  failbackEnabled.add(nc);

  const intervalMs = positiveNumber(process.env.NATS_FAILBACK_INTERVAL_MS, 30_000);
  const required = positiveNumber(process.env.NATS_FAILBACK_SUCCESSES, 3);
  const timeoutMs = positiveNumber(process.env.NATS_FAILBACK_PROBE_TIMEOUT_MS, 1_000);
  const healthURL =
    process.env.NATS_LOCAL_LEAF_HEALTH_URL || 'http://nats-leaf-local:8222/healthz';
  let consecutive = 0;
  let running = false;

  const check = async () => {
    if (running || nc.isClosed()) return;
    running = true;
    try {
      const serverName = nc.info?.server_name ?? '';
      if (serverName.startsWith(`${nodeName}--`)) {
        consecutive = 0;
        return;
      }
      if (!(await localLeafReady(healthURL, timeoutMs))) {
        consecutive = 0;
        return;
      }
      consecutive++;
      if (consecutive < required) return;
      consecutive = 0;
      failbackQueue = failbackQueue.then(async () => {
        try {
          if (!nc.isClosed() && !nc.info?.server_name?.startsWith(`${nodeName}--`)) {
            await nc.reconnect();
          }
        } catch {}
      });
      await failbackQueue;
    } catch {
      consecutive = 0;
    } finally {
      running = false;
    }
  };

  const initial = setTimeout(() => {
    void check();
    const timer = setInterval(() => void check(), intervalMs);
    timer.unref();
  }, Math.random() * intervalMs);
  initial.unref();
}

function fallbackServer(override: string | undefined): string {
  // `||`, not `??`: a blank NATS_HOST would build 'nats://:4222'.
  return override || `nats://${process.env.NATS_HOST || '127.0.0.1'}:${process.env.NATS_PORT || '4222'}`;
}

function rpcServerList(override: string | undefined): string[] {
  return [fallbackServer(override)];
}

function busServerList(override: string | undefined): string[] {
  return [process.env.NATS_HUB_URL || fallbackServer(override)];
}

export function tlsOptions(): ConnectionOptions['tls'] | undefined {
  const caPem = process.env.NATS_CA_PEM;
  if (!caPem) return undefined;

  const certFile = process.env.NATS_CLIENT_CERT_FILE;
  const keyFile = process.env.NATS_CLIENT_KEY_FILE;
  if (!!certFile !== !!keyFile) {
    throw new Error('NATS_CLIENT_CERT_FILE and NATS_CLIENT_KEY_FILE must both be set or both empty');
  }
  if (certFile && keyFile) return { ca: caPem, certFile, keyFile };
  return { ca: caPem };
}

// `||`, not `??`: a set-but-blank Doppler var must fall back, not authenticate with ''.
function roleEnv(
  isRpc: boolean,
  rpcVar: string | undefined,
  sharedVar: string | undefined
): string | undefined {
  return isRpc ? rpcVar || sharedVar : sharedVar;
}

interface CredentialAuth {
  authenticator?: Authenticator[];
}

export function credentialAuth(jwt: string | undefined, nkeySeed: string | undefined): CredentialAuth {
  if (!jwt || !nkeySeed) return {};
  return { authenticator: [jwtAuthenticator(jwt, new TextEncoder().encode(nkeySeed))] };
}

export function options(role: Role): ConnectionOptions {
  const isRpc = role === 'rpc';
  const opts: ConnectionOptions = {
    servers: isRpc
      ? rpcServerList(process.env.NATS_RPC_URL)
      : busServerList(process.env.NATS_URL),
    noRandomize: true,
    name: `${process.env.NATS_CLIENT_NAME || 'console'}-${role}`,
    maxReconnectAttempts: -1,
    reconnectTimeWait: 500,
    ignoreAuthErrorAbort: true,
    pingInterval: 20_000,
    timeout: 3_000
  };
  const jwt = roleEnv(isRpc, process.env.NATS_RPC_JWT, process.env.NATS_JWT);
  const nkeySeed = roleEnv(isRpc, process.env.NATS_RPC_NKEY_SEED, process.env.NATS_NKEY_SEED);
  Object.assign(opts, credentialAuth(jwt, nkeySeed));
  if (process.env.NATS_TOKEN) opts.token = process.env.NATS_TOKEN;
  const tls = tlsOptions();
  if (tls) opts.tls = tls;
  return opts;
}

async function get(role: Role): Promise<NatsConnection> {
  const pool = pools[role];
  if (pool.conn && !pool.conn.isClosed()) return pool.conn;
  if (pool.dialing) return pool.dialing;
  pool.dialing = connect(options(role))
    .then((c) => {
      if (c.isClosed()) throw new Error('nats connection closed during dial');
      pool.conn = c;
      if (role === 'bus') {
        jsClient = null;
        jsManager = null;
      }
      if (role === 'rpc') enableLeafFailback(c);
      return c;
    })
    .finally(() => {
      pool.dialing = null;
    });
  return pool.dialing;
}

let jsClient: JetStreamClient | null = null;
let jsManager: JetStreamManager | null = null;

// Not domain:'hub': its alias is remapped after the permission check and every lane request is denied.
export function hubJetStreamOptions(): JetStreamManagerOptions {
  return { apiPrefix: '$JS.API', checkAPI: false };
}

export async function js(): Promise<JetStreamClient> {
  const nc = await get('bus');
  if (!jsClient) jsClient = jetstream(nc, hubJetStreamOptions());
  return jsClient;
}

export async function jsm(): Promise<JetStreamManager> {
  const nc = await get('bus');
  if (!jsManager) jsManager = await jetstreamManager(nc, hubJetStreamOptions());
  return jsManager;
}

export function warm(): void {
  get('rpc').catch(() => {});
  get('bus').catch(() => {});
}

async function within<T>(promise: Promise<T>, timeoutMs: number): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error('timeout')), timeoutMs);
  });
  try {
    return await Promise.race([promise, timeout]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

export async function ready(timeoutMs = 750): Promise<boolean> {
  try {
    const nc = await within(get('rpc'), timeoutMs);
    await within(nc.flush(), timeoutMs);
    return !nc.isClosed();
  } catch {
    return false;
  }
}

export class RpcError extends Error {
  readonly code: RpcCode | '';

  constructor(message: string, code: RpcCode | '' = '') {
    super(message);
    this.code = code;
  }
}

export async function rpc<T>(subject: string, payload: unknown = {}, timeoutMs = 5000): Promise<T> {
  const reply = await rpcReply<T>(subject, payload, timeoutMs);
  const refusal = rpcRefusal(reply);
  if (refusal) throw refusal;
  return reply;
}

export function rpcRefusal(reply: unknown): RpcError | null {
  if (!reply || typeof reply !== 'object') return null;
  const r = reply as CodedReply;
  if (!r.error) return null;
  return new RpcError(r.error, rpcCode(r));
}

export async function rpcReply<T>(subject: string, payload: unknown = {}, timeoutMs = 5000): Promise<T> {
  return newrelic.startSegment(`NATS/request/${rpcSegment(subject)}`, true, async () => {
    const nc = await get('rpc');
    const subjects = rpcSubjectsForNode(subject, process.env.NODE_NAME);
    const data = JSON.stringify(payload);
    const msg = await requestLocalFirst(subjects, (routedSubject) =>
      nc.request(routedSubject, data, { timeout: timeoutMs })
    );
    return msg.json<T>();
  });
}

export async function publish(subject: string, payload: unknown = {}): Promise<void> {
  return newrelic.startSegment(`NATS/publish/${rpcSegment(subject)}`, true, async () => {
    const role: Role = subject.startsWith('twitch.') || subject.startsWith('data.') ? 'bus' : 'rpc';
    const nc = await get(role);
    nc.publish(subject, JSON.stringify(payload));
    await nc.flush();
  });
}

export async function closeNats(): Promise<void> {
  for (const pool of Object.values(pools)) {
    if (pool.conn && !pool.conn.isClosed()) await pool.conn.drain();
    pool.conn = null;
  }
  jsClient = null;
  jsManager = null;
}

export function subscribe(subject: string, onMsg: (subject: string, data: Uint8Array) => void): void {
  get('rpc')
    .then((nc) => {
      const sub = nc.subscribe(subject);
      (async () => {
        try {
          for await (const m of sub) onMsg(m.subject, m.data);
        } catch {}
      })();
    })
    .catch(() => {});
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    const t = setTimeout(resolve, ms);
    t.unref();
  });
}

function backoffMs(attempt: number): number {
  const base = Math.min(500 * 2 ** Math.min(attempt, 6), 30_000);
  return base / 2 + Math.random() * (base / 2);
}

export function subscribeDurable(
  subject: string,
  onMsg: (subject: string, data: Uint8Array) => void,
  onGap?: () => void
): void {
  const gap = () => {
    try {
      onGap?.();
    } catch {}
  };

  void (async () => {
    let attempt = 0;
    for (;;) {
      let nc: NatsConnection;
      try {
        nc = await get('rpc');
      } catch {
        attempt++;
        newrelic.recordMetric('Custom/NatsBus/dial_retry', 1);
        await sleep(backoffMs(attempt));
        continue;
      }

      if (attempt > 0) {
        newrelic.recordMetric('Custom/NatsBus/gap_flush', 1);
        gap();
      }
      attempt = 0;

      void (async () => {
        try {
          for await (const s of nc.status()) {
            if (s.type === 'reconnect') {
              newrelic.recordMetric('Custom/NatsBus/gap_flush', 1);
              gap();
            }
          }
        } catch {}
      })();

      try {
        const sub = nc.subscribe(subject);
        for await (const m of sub) {
          try {
            onMsg(m.subject, m.data);
          } catch {}
        }
      } catch {}

      attempt++;
      newrelic.recordMetric('Custom/NatsBus/resubscribe', 1);
      await sleep(backoffMs(attempt));
    }
  })();
}
