// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync } from 'node:fs';
import type { ConnectionOptions } from 'node:tls';
import type { ValkeyConfig } from './config';

export const VALKEY_TLS_DATA_PORT = 6380;
export const VALKEY_TLS_SENTINEL_PORT = 26380;

// familyFallback off: after a lost connection iovalkey flips family to 6, a Sentinel resolve loop never flips it back (no close event), every lookup is ENOTFOUND and Bun keeps each failed socket: ~1 MiB/min until OOM after every failover (2026-10-15).
export const VALKEY_CLIENT_OPTIONS = {
  enableOfflineQueue: false,
  maxRetriesPerRequest: 1,
  connectTimeout: 1000,
  familyFallback: { enabled: false },
  enableAutoPipelining: true,
  retryStrategy: (times: number) => Math.min(times * 200, 2000)
} as const;
const DEFAULT_SERVER_NAME = 'valkey.valkey.svc.cluster.local';

export function valkeyTLSOptions(cfg: ValkeyConfig): ConnectionOptions | undefined {
  if (!cfg.tlsCa) return undefined;
  const options: ConnectionOptions = {
    ca: cfg.tlsCa,
    servername: cfg.tlsServerName || DEFAULT_SERVER_NAME,
    minVersion: 'TLSv1.2'
  };
  // Getters, not Buffers: iovalkey re-copies them per reconnect, so a rotated cert applies without a restart.
  if (cfg.tlsClientCertFile && cfg.tlsClientKeyFile) {
    const certFile = cfg.tlsClientCertFile;
    const keyFile = cfg.tlsClientKeyFile;
    Object.defineProperties(options, {
      cert: { enumerable: true, get: () => readFileSync(certFile) },
      key: { enumerable: true, get: () => readFileSync(keyFile) }
    });
  }
  return options;
}

export function valkeyEndpoint(
  address: string,
  tlsEnabled: boolean,
  tlsPort: number
): { host: string; port: number } {
  const separator = address.lastIndexOf(':');
  const host = separator >= 0 ? address.slice(0, separator) : address;
  const parsedPort = separator >= 0 ? Number(address.slice(separator + 1)) : 0;
  return {
    host: host || '127.0.0.1',
    port: tlsEnabled ? tlsPort : parsedPort || (tlsPort === VALKEY_TLS_SENTINEL_PORT ? 26379 : 6379)
  };
}

export function valkeySentinelNAT(key: string): { host: string; port: number } | null {
  const separator = key.lastIndexOf(':');
  if (separator < 0 || key.slice(separator + 1) !== '6379') return null;
  return { host: key.slice(0, separator), port: VALKEY_TLS_DATA_PORT };
}
