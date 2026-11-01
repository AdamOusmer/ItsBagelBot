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

export async function localLeafReady(
  healthURL: string,
  timeoutMs: number,
  ca: string | undefined = process.env.NATS_CA_PEM
): Promise<boolean> {
  try {
    const response = await fetch(healthURL, {
      signal: AbortSignal.timeout(timeoutMs),
      cache: 'no-store',
      ...(ca ? { tls: { ca } } : {})
    });
    await response.body?.cancel();
    return response.ok;
  } catch {
    return false;
  }
}

const MAX_FAILBACK_BACKOFF_SHIFT = 4;

export type Locality = 'local' | 'displaced' | 'unknown';

// Mirrors pkg/bus/failback.go: a failback that lands remote again doubles the streak it needs next time, capped at 16x.
export class FailbackStreak {
  private consecutive = 0;
  private failbacks = 0;

  constructor(private readonly required: number) {}

  observe(current: Locality): boolean {
    if (current === 'local') {
      this.consecutive = 0;
      this.failbacks = 0;
      return false;
    }
    if (current === 'unknown') {
      this.consecutive = 0;
      return false;
    }
    this.consecutive++;
    if (this.consecutive < this.required * 2 ** Math.min(this.failbacks, MAX_FAILBACK_BACKOFF_SHIFT)) {
      return false;
    }
    this.consecutive = 0;
    this.failbacks++;
    return true;
  }
}

interface FailbackSettings {
  intervalMs: number;
  required: number;
  timeoutMs: number;
}

function failbackSettings(): FailbackSettings {
  return {
    intervalMs: positiveNumber(process.env.NATS_FAILBACK_INTERVAL_MS, 30_000),
    required: positiveNumber(process.env.NATS_FAILBACK_SUCCESSES, 3),
    timeoutMs: positiveNumber(process.env.NATS_FAILBACK_PROBE_TIMEOUT_MS, 1_000)
  };
}

function runFailback(
  nc: NatsConnection,
  settings: FailbackSettings,
  place: (nc: NatsConnection) => Promise<Locality>,
  isLocal: (nc: NatsConnection) => boolean
): void {
  const streak = new FailbackStreak(settings.required);
  let running = false;
  let timer: ReturnType<typeof setInterval> | undefined;

  const check = async () => {
    if (nc.isClosed()) {
      if (timer) clearInterval(timer);
      return;
    }
    if (running) return;
    running = true;
    try {
      let current: Locality = 'unknown';
      try {
        current = await place(nc);
      } catch {}
      if (!streak.observe(current)) return;
      failbackQueue = failbackQueue.then(async () => {
        try {
          // A queued failback can find the connection already back home.
          if (!nc.isClosed() && !isLocal(nc)) await nc.reconnect();
        } catch {}
      });
      await failbackQueue;
    } finally {
      running = false;
    }
  };

  const initial = setTimeout(() => {
    void check();
    timer = setInterval(() => void check(), settings.intervalMs);
    timer.unref();
  }, Math.random() * settings.intervalMs);
  initial.unref();
}

export function enableLeafFailback(nc: NatsConnection): void {
  const nodeName = process.env.NODE_NAME;
  if (!nodeName || failbackEnabled.has(nc)) return;
  failbackEnabled.add(nc);

  const settings = failbackSettings();
  const healthURL =
    process.env.NATS_LOCAL_LEAF_HEALTH_URL || 'https://nats-leaf-local.messaging:8222/healthz';

  const isLocal = (nc: NatsConnection) => (nc.info?.server_name ?? '').startsWith(`${nodeName}--`);

  runFailback(
    nc,
    settings,
    async (nc) => {
      if (isLocal(nc)) return 'local';
      return (await localLeafReady(healthURL, settings.timeoutMs)) ? 'displaced' : 'unknown';
    },
    isLocal
  );
}

interface HubProbeState {
  name: string;
  checkedAt: number;
  inflight: Promise<string> | null;
}

const localHub: HubProbeState = { name: '', checkedAt: Number.NEGATIVE_INFINITY, inflight: null };

export function resetLocalHubProbe(): void {
  localHub.name = '';
  localHub.checkedAt = Number.NEGATIVE_INFINITY;
  localHub.inflight = null;
}

// The hub's monitor port stays closed to app namespaces, so locality is read from a client handshake.
export async function probeHubName(base: ConnectionOptions, url: string, timeoutMs: number): Promise<string> {
  let probe: NatsConnection | undefined;
  try {
    probe = await connect({
      ...base,
      servers: [url],
      name: 'hub-failback-probe',
      reconnect: false,
      maxReconnectAttempts: 0,
      timeout: timeoutMs
    });
    return probe.info?.server_name ?? '';
  } catch {
    return '';
  } finally {
    await probe?.close().catch(() => {});
  }
}

// One probe per process per interval, shared by every hub connection.
async function localHubName(
  connected: string,
  base: ConnectionOptions,
  url: string,
  settings: FailbackSettings
): Promise<string> {
  if (connected && connected === localHub.name) return localHub.name;
  if (localHub.inflight) return localHub.inflight;
  if (Date.now() - localHub.checkedAt < settings.intervalMs) return localHub.name;

  localHub.checkedAt = Date.now();
  localHub.name = '';
  localHub.inflight = probeHubName(base, url, settings.timeoutMs)
    .then((name) => {
      localHub.name = name;
      return name;
    })
    .finally(() => {
      localHub.inflight = null;
    });
  return localHub.inflight;
}

export function enableHubFailback(nc: NatsConnection, base: ConnectionOptions): void {
  if (!process.env.NODE_NAME || !process.env.NATS_HUB_URL || failbackEnabled.has(nc)) return;
  failbackEnabled.add(nc);

  const settings = failbackSettings();
  // Coupled to the nats-local Service (internalTrafficPolicy: Local) in deploy/messaging/nats.yaml.
  const url = process.env.NATS_LOCAL_HUB_URL || 'tls://nats-local.messaging:4222';

  runFailback(
    nc,
    settings,
    async (nc) => {
      const connected = nc.info?.server_name ?? '';
      if (!connected) return 'unknown';
      const local = await localHubName(connected, base, url, settings);
      if (!local) return 'unknown';
      return local === connected ? 'local' : 'displaced';
    },
    (nc) => !!localHub.name && nc.info?.server_name === localHub.name
  );
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
    // Ordered on the RPC plane only: leaf first, hub second. Unshuffled BUS pools ended 0/5/222 across nats-0/1/2 after a hub roll (2026-10-11).
    noRandomize: isRpc,
    // Discovered hub pod IPs bypass Service readiness; dialing them piled 222 of 227 BUS clients on one hub (2026-10-11).
    ignoreClusterUpdates: true,
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
      if (role === 'bus') enableHubFailback(c, options('bus'));
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

export interface PublishOptions {
  // Waits for the server to receive the message, for callers whose next step on another connection must land after it.
  flush?: boolean;
}

export async function publish(subject: string, payload: unknown = {}, opts: PublishOptions = {}): Promise<void> {
  return newrelic.startSegment(`NATS/publish/${rpcSegment(subject)}`, true, async () => {
    const role: Role = subject.startsWith('twitch.') || subject.startsWith('data.') ? 'bus' : 'rpc';
    const nc = await get(role);
    nc.publish(subject, JSON.stringify(payload));
    if (opts.flush) await nc.flush();
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
        newrelic.recordMetric('NatsBus/dial_retry', 1);
        await sleep(backoffMs(attempt));
        continue;
      }

      if (attempt > 0) {
        newrelic.recordMetric('NatsBus/gap_flush', 1);
        gap();
      }
      attempt = 0;

      void (async () => {
        try {
          for await (const s of nc.status()) {
            if (s.type === 'reconnect') {
              newrelic.recordMetric('NatsBus/gap_flush', 1);
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
      newrelic.recordMetric('NatsBus/resubscribe', 1);
      await sleep(backoffMs(attempt));
    }
  })();
}
