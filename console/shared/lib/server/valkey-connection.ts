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
  // mTLS, reloaded per reconnect rather than read once: iovalkey's connectors
  // (StandaloneConnector and SentinelConnector) hold onto this exact `tls`
  // object for the client's whole lifetime and Object.assign it into the
  // socket options fresh on every connect() call -- i.e. on every reconnect,
  // which is this long-lived client's equivalent of "the next handshake."
  // Object.assign invokes property getters at copy time, so defining cert/key
  // as getters (not static Buffers captured once here) means the mounted
  // Secret volume is re-read at that moment. A cert-manager rotation at day
  // 75 reaches the client on its next reconnect with no process restart --
  // the same fix as the Go client's GetClientCertificate, expressed through
  // this library's own re-copy mechanism since Node's tls module has no
  // per-handshake callback for client certs (only servers get SNICallback).
  // No mtime cache here unlike the Go side: reconnects are rare (network
  // blips, breaker resets), not a per-request hot path, so re-reading two
  // small files each time costs nothing worth guarding against.
  // Both-or-neither, matching the shared Go client's
  // VALKEY_TLS_CLIENT_CERT_FILE/KEY_FILE contract.
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

// During the guarded dual-listener rollout an older Sentinel may briefly
// report the plaintext data port. A TLS-enabled client always translates that
// stale endpoint to the native TLS listener; current 6380 replies pass through.
export function valkeySentinelNAT(key: string): { host: string; port: number } | null {
  const separator = key.lastIndexOf(':');
  if (separator < 0 || key.slice(separator + 1) !== '6379') return null;
  return { host: key.slice(0, separator), port: VALKEY_TLS_DATA_PORT };
}
