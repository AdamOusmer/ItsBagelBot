// One-time boot sequence shared by both consoles' init hooks: DNS ordering,
// config sanity, the caching-layer registry and the NATS pre-dial. Each app
// calls this from its own SvelteKit init() and then layers its app-specific
// warmups (lane-store HA, Valkey pools, rate limiters) on top.

import dns from 'node:dns';
import { registerServerConfig } from './config';
import { warm } from './nats';

type Env = Record<string, string | undefined>;

/** Runs the boot steps common to both consoles.
 *
 *  Reads the passed env (the apps pass process.env, NOT $env/dynamic/private:
 *  init() runs under the server entry's top-level `await server.init()`, so
 *  reading the dynamic-env proxy there deadlocks that await — unsettled
 *  top-level await -> exit 13. In adapter-node process.env carries the same
 *  Doppler-injected runtime values.) */
export function initConsoleRuntime(env: Env, assertConfigSane: (env: Env) => void): void {
  // Force node:dns to resolve IPv4 first to bypass k3s IPv6 timeout issues.
  dns.setDefaultResultOrder('ipv4first');

  assertConfigSane(env);

  // Register the caching-layer config (Valkey read tier + invalidation bus) so
  // shared infra resolves it without touching $env itself. The Sentinel fields
  // are optional; apps without a Sentinel write path simply don't set them.
  registerServerConfig({
    valkey: env.VALKEY_ADDR
      ? {
          addr: env.VALKEY_ADDR,
          password: env.VALKEY_PASSWORD,
          sentinelAddr: env.VALKEY_SENTINEL_ADDR,
          sentinelMaster: env.VALKEY_MASTER_SET,
          tlsCa: env.VALKEY_TLS_CA_PEM,
          tlsServerName: env.VALKEY_TLS_SERVER_NAME
        }
      : undefined,
    cacheInvalidationPrefix: env.NATS_CACHE_INVALIDATION_PREFIX ?? 'bagel.cache.invalidate'
  });

  // Pre-dial NATS so the first request hits a warm connection instead of
  // paying the cold dial on the hot path.
  warm();
}
