// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync } from 'node:fs';
import type { ConnectionOptions } from 'node:tls';
import type { ValkeyConfig } from './config';

export const VALKEY_TLS_DATA_PORT = 6380;
export const VALKEY_TLS_SENTINEL_PORT = 26380;
const DEFAULT_SERVER_NAME = 'valkey.valkey.svc.cluster.local';

export function valkeyTLSOptions(cfg: ValkeyConfig): ConnectionOptions | undefined {
  if (!cfg.tlsCa) return undefined;
  const options: ConnectionOptions = {
    ca: cfg.tlsCa,
    servername: cfg.tlsServerName || DEFAULT_SERVER_NAME,
    minVersion: 'TLSv1.2'
  };
  // mTLS: read from the mounted Secret volume at connection-build time (this
  // function runs from initConsoleRuntime's boot step, never at module eval,
  // so it is not subject to the $env/dynamic/private top-level-await
  // deadlock this file's callers already avoid). Both-or-neither, matching
  // the shared Go client's VALKEY_TLS_CLIENT_CERT_FILE/KEY_FILE contract.
  if (cfg.tlsClientCertFile && cfg.tlsClientKeyFile) {
    options.cert = readFileSync(cfg.tlsClientCertFile);
    options.key = readFileSync(cfg.tlsClientKeyFile);
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

// During the guarded dual-listener rollout an older Sentinel may briefly
// report the plaintext data port. A TLS-enabled client always translates that
// stale endpoint to the native TLS listener; current 6380 replies pass through.
export function valkeySentinelNAT(key: string): { host: string; port: number } | null {
  const separator = key.lastIndexOf(':');
  if (separator < 0 || key.slice(separator + 1) !== '6379') return null;
  return { host: key.slice(0, separator), port: VALKEY_TLS_DATA_PORT };
}
