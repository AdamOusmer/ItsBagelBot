import type { ConnectionOptions } from 'node:tls';
import type { ValkeyConfig } from './config';

export const VALKEY_TLS_DATA_PORT = 6380;
export const VALKEY_TLS_SENTINEL_PORT = 26380;
const DEFAULT_SERVER_NAME = 'valkey.valkey.svc.cluster.local';

export function valkeyTLSOptions(cfg: ValkeyConfig): ConnectionOptions | undefined {
  if (!cfg.tlsCa) return undefined;
  return {
    ca: cfg.tlsCa,
    servername: cfg.tlsServerName || DEFAULT_SERVER_NAME,
    minVersion: 'TLSv1.2'
  };
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
