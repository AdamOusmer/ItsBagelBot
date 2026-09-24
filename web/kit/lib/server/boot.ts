// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import dns from 'node:dns';
import { registerServerConfig } from './config';
import { warm } from './nats';

type Env = Record<string, string | undefined>;

/** Pass process.env: reading $env/dynamic/private under init()'s top-level await deadlocks boot. */
export function initConsoleRuntime(env: Env, assertConfigSane: (env: Env) => void): void {
  // IPv4 first: IPv6 lookups time out in the k3s cluster.
  dns.setDefaultResultOrder('ipv4first');

  assertConfigSane(env);

  registerServerConfig({
    valkey: env.VALKEY_ADDR
      ? {
          addr: env.VALKEY_ADDR,
          password: env.VALKEY_PASSWORD,
          sentinelAddr: env.VALKEY_SENTINEL_ADDR,
          sentinelMaster: env.VALKEY_MASTER_SET,
          tlsCa: env.VALKEY_TLS_CA_PEM,
          tlsServerName: env.VALKEY_TLS_SERVER_NAME,
          tlsClientCertFile: env.VALKEY_TLS_CLIENT_CERT_FILE,
          tlsClientKeyFile: env.VALKEY_TLS_CLIENT_KEY_FILE
        }
      : undefined,
    cacheInvalidationPrefix: env.NATS_CACHE_INVALIDATION_PREFIX ?? 'bagel.cache.invalidate'
  });

  warm();
}
