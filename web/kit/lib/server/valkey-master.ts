// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import Redis from 'iovalkey';
import {
  VALKEY_TLS_DATA_PORT,
  VALKEY_TLS_SENTINEL_PORT,
  valkeyEndpoint,
  valkeySentinelNAT,
  valkeyTLSOptions,
  VALKEY_CLIENT_OPTIONS
} from './valkey-connection';
import { getServerConfig, hasServerConfig } from './config';
import { logger } from './logger';

// A Sentinel client that loses its master in a failover can stay out of `ready`
// for good (2026-09-24: 40 min until a pod restart); a fresh one finds the new master.
const STUCK_REBUILD_MS = 30_000;
const ERROR_LOG_EVERY_MS = 60_000;

let client: Redis | null = null;
let disabled = false;
let notReadySince = 0;
let lastErrorLog = 0;

type ValkeyConfig = NonNullable<ReturnType<typeof getServerConfig>['valkey']>;
type TLSOptions = ReturnType<typeof valkeyTLSOptions>;

function sentinelOptions(cfg: ValkeyConfig, tls: TLSOptions) {
  return {
    sentinels: [valkeyEndpoint(cfg.sentinelAddr as string, Boolean(tls), VALKEY_TLS_SENTINEL_PORT)],
    // `||`, not `??`: a blank VALKEY_MASTER_SET never resolves a master and every write times out.
    name: cfg.sentinelMaster || 'myprimary',
    password: cfg.password || undefined,
    sentinelPassword: cfg.password || undefined,
    tls,
    sentinelTLS: tls,
    enableTLSForSentinelMode: Boolean(tls),
    natMap: tls ? valkeySentinelNAT : undefined,
    sentinelRetryStrategy: (times: number) => Math.min(times * 200, 2000),
    ...VALKEY_CLIENT_OPTIONS
  };
}

function directOptions(cfg: ValkeyConfig, tls: TLSOptions) {
  const endpoint = valkeyEndpoint(cfg.addr, Boolean(tls), VALKEY_TLS_DATA_PORT);
  return { host: endpoint.host, port: endpoint.port, password: cfg.password || undefined, tls, ...VALKEY_CLIENT_OPTIONS };
}

function logError(err: Error): void {
  const now = Date.now();
  if (now - lastErrorLog < ERROR_LOG_EVERY_MS) return;
  lastErrorLog = now;
  logger.warn({ err: err.message }, 'valkey master client error');
}

function rebuildIfStuck(now: number): void {
  if (!client || client.status === 'ready') {
    notReadySince = 0;
    return;
  }
  if (notReadySince === 0) notReadySince = now;
  if (now - notReadySince < STUCK_REBUILD_MS) return;
  logger.warn({ status: client.status }, 'valkey master client stuck, rebuilding');
  client.disconnect();
  client = null;
}

export function masterClient(): Redis | null {
  if (disabled) return null;
  rebuildIfStuck(Date.now());
  if (client) return client;
  const cfg = hasServerConfig() ? getServerConfig().valkey : undefined;
  if (!cfg) {
    disabled = true;
    return null;
  }

  const tls = valkeyTLSOptions(cfg);
  const c = new Redis(cfg.sentinelAddr ? sentinelOptions(cfg, tls) : directOptions(cfg, tls));
  c.on('error', logError);
  client = c;
  notReadySince = Date.now();
  return client;
}

export function warmMasterClient(): void {
  masterClient();
}

export function resetMasterClientForTests(): void {
  client?.disconnect();
  client = null;
  disabled = false;
  notReadySince = 0;
}
