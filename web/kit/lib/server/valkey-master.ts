// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

type ValkeyConfig = NonNullable<ReturnType<typeof getServerConfig>['valkey']>;
type TLSOptions = ReturnType<typeof valkeyTLSOptions>;

const FAIL_FAST = {
  enableOfflineQueue: false,
  maxRetriesPerRequest: 1,
  connectTimeout: 1000,
  retryStrategy: (times: number) => Math.min(times * 200, 2000)
} as const;

function sentinelOptions(cfg: ValkeyConfig, tls: TLSOptions) {
  return {
    sentinels: [valkeyEndpoint(cfg.sentinelAddr as string, Boolean(tls), VALKEY_TLS_SENTINEL_PORT)],
    // `||`, not `??`: a blank VALKEY_MASTER_SET never resolves a master and revocation writes time out.
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

export function masterClient(): Redis | null {
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

export function warmMasterClient(): void {
  masterClient();
}

export function resetMasterClientForTests(): void {
  client?.disconnect();
  client = null;
  disabled = false;
}
