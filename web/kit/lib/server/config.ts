// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface ValkeyConfig {
  addr: string;
  password?: string;
  sentinelAddr?: string;
  sentinelMaster?: string;
  tlsCa?: string;
  tlsServerName?: string;
  tlsClientCertFile?: string;
  tlsClientKeyFile?: string;
}

export interface ServerConfig {
  valkey?: ValkeyConfig;
  cacheInvalidationPrefix: string;
}

let current: ServerConfig | null = null;

export function registerServerConfig(cfg: ServerConfig): void {
  current = cfg;
}

export function hasServerConfig(): boolean {
  return current !== null;
}

export function getServerConfig(): ServerConfig {
  if (!current) {
    throw new Error('server config not registered; call registerServerConfig() in the init hook');
  }
  return current;
}
