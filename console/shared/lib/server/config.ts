// Framework-agnostic server-config registry.
//
// SvelteKit's `$env/dynamic/private` is only safe to read at request time, never
// at module-eval of a module in the boot import graph (doing so deadlocks the
// handler import -> exit 13). The apps therefore read `$env/dynamic/private` in
// their `init` server hook (which runs once, after env is ready) and register
// the resolved values here; the shared infra (NATS client, Valkey store, cache)
// reads them back through `getServerConfig()`.
//
// This keeps `$env` resolution inside the app, where SvelteKit guarantees it,
// and keeps `@bagel/shared` free of SvelteKit virtual-module imports (a clean
// dependency-inversion seam: shared depends on this abstraction, not on the
// framework).
//
// Scope: the caching layer (Valkey read tier + invalidation bus). The NATS
// connection client owns its own dial-time env (read at request time, which is
// deadlock-safe); it is intentionally not routed through here.

export interface ValkeyConfig {
  /** host:port of the node-local read instance, e.g. "127.0.0.1:6380". */
  addr: string;
  password?: string;
  /**
   * host:port of a Sentinel endpoint, e.g. "valkey.valkey.svc:26380". When set,
   * write-path clients (rate limiter) connect through Sentinel so they always
   * track the elected master across failovers; when absent, writes go to
   * `addr` directly (single-instance/dev setups).
   */
  sentinelAddr?: string;
  /** Sentinel master set name. Defaults to "myprimary" (the fleet's set). */
  sentinelMaster?: string;
  /** Fleet CA PEM enables verified native TLS for data and Sentinel sockets. */
  tlsCa?: string;
  /** Expected server identity; defaults to the stable in-cluster Service DNS. */
  tlsServerName?: string;
}

export interface ServerConfig {
  /** Absent disables the Valkey read tier (degrades to projector RPC). */
  valkey?: ValkeyConfig;
  /** Cache-invalidation bus subject prefix, e.g. "bagel.cache.invalidate". */
  cacheInvalidationPrefix: string;
}

let current: ServerConfig | null = null;

/** Register the resolved server config. Call once from the app `init` hook. */
export function registerServerConfig(cfg: ServerConfig): void {
  current = cfg;
}

/** True once {@link registerServerConfig} has run (i.e. after `init`). */
export function hasServerConfig(): boolean {
  return current !== null;
}

/**
 * Read the registered server config. Throws if called before the `init` hook
 * registered it (a programming error, surfaced loudly rather than silently
 * falling back to unset env).
 */
export function getServerConfig(): ServerConfig {
  if (!current) {
    throw new Error('server config not registered; call registerServerConfig() in the init hook');
  }
  return current;
}
