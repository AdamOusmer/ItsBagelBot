// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Sentinel/master-pinned Valkey write client, dedicated to session-revocation
// writes (revokeSession, revokeAllForUser in session-revocation.ts). A
// revocation write MUST land on the elected master: pointing it at the
// node-local replica (valkey-store.ts's read client) would let "sign out
// everywhere" or a single-session revoke lose the race with replication and
// silently never take effect.
//
// This deliberately does NOT share a connection with rate-limit.ts's own
// master-pinned write client, even though the construction is nearly
// identical (same low-level helpers below). Each critical-path write
// dependency gets its own client + circuit breaker, so a fault, reconnect
// storm, or config change on one never perturbs the other — the same
// per-dependency isolation resilience.ts's CircuitBreaker doc argues for.
import Redis from 'iovalkey';
import {
  VALKEY_TLS_DATA_PORT,
  VALKEY_TLS_SENTINEL_PORT,
  valkeyEndpoint,
  valkeySentinelNAT,
  valkeyTLSOptions
} from './valkey-connection';
import { getServerConfig, hasServerConfig } from './config';

let client: Redis | null = null;
let disabled = false;

/**
 * Lazily build (or return the cached) Sentinel-pinned master client. Null
 * when Valkey is unconfigured (dev, unit tests, misordered boot) — callers
 * degrade rather than throw. Breaker + per-op timeout are the caller's job
 * (see session-revocation.ts), matching how valkey-store.ts and rate-limit.ts
 * each own their own resilience around their own client.
 */
export function masterClient(): Redis | null {
  if (disabled) return null;
  if (client) return client;
  // Config is registered by the init hook; tolerate its absence (unit tests,
  // misordered boot) by returning null instead of throwing.
  const cfg = hasServerConfig() ? getServerConfig().valkey : undefined;
  if (!cfg) {
    disabled = true;
    return null;
  }

  const tls = valkeyTLSOptions(cfg);
  let c: Redis;
  if (cfg.sentinelAddr) {
    const endpoint = valkeyEndpoint(cfg.sentinelAddr, Boolean(tls), VALKEY_TLS_SENTINEL_PORT);
    c = new Redis({
      sentinels: [endpoint],
      // `||` not `??`: an empty VALKEY_MASTER_SET (unset in Doppler comes
      // through as "") must fall back to the sentinel's monitored name, not
      // be used verbatim — a blank master name never resolves, so every
      // revocation write would silently time out.
      name: cfg.sentinelMaster || 'myprimary',
      password: cfg.password || undefined,
      // The sentinels carry the same requirepass as the data nodes (ACL
      // default user with a password hash), and ioredis authenticates the
      // sentinel hop separately from the master connection. Without this the
      // sentinel answers NOAUTH, the client never resolves a master, and
      // every write silently degrades to fail-open.
      sentinelPassword: cfg.password || undefined,
      tls,
      sentinelTLS: tls,
      enableTLSForSentinelMode: Boolean(tls),
      natMap: tls ? valkeySentinelNAT : undefined,
      // Fail fast, never queue: an unreachable master must degrade
      // immediately, not grow an unbounded command queue.
      enableOfflineQueue: false,
      maxRetriesPerRequest: 1,
      connectTimeout: 1000,
      retryStrategy: (times) => Math.min(times * 200, 2000),
      sentinelRetryStrategy: (times: number) => Math.min(times * 200, 2000)
    });
  } else {
    const endpoint = valkeyEndpoint(cfg.addr, Boolean(tls), VALKEY_TLS_DATA_PORT);
    c = new Redis({
      host: endpoint.host,
      port: endpoint.port,
      password: cfg.password || undefined,
      tls,
      enableOfflineQueue: false,
      maxRetriesPerRequest: 1,
      connectTimeout: 1000,
      retryStrategy: (times) => Math.min(times * 200, 2000)
    });
  }

  // Swallow connection errors; ops fail fast under the caller's own breaker +
  // timeout and this client reconnects in the background.
  c.on('error', () => {});
  client = c;
  return client;
}

/**
 * Pre-connect the master client at boot so the first revocation write does
 * not pay the dial. Best-effort, no-op when Valkey is unconfigured. Call
 * from the init hook (alongside warmRateLimiter / valkey-store's warm).
 */
export function warmMasterClient(): void {
  masterClient();
}

/**
 * Drop the cached client and its disabled latch so a test can re-run client
 * resolution against fresh config. Test-only; never call in app code.
 */
export function resetMasterClientForTests(): void {
  client?.disconnect();
  client = null;
  disabled = false;
}
