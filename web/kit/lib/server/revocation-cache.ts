// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RevocationKeys } from './valkey-store';

export type RevocationRead = [string | null, string | null];

export interface RevocationClaims extends RevocationKeys {
  iat: number;
}

// Cross-pod revocations reach this pod only through Valkey, so this is how long another pod's revoke can lag here.
const READ_CACHE_MS = 2_000;
const READ_CACHE_MAX = 10_000;
// Outlives a cached read plus replica lag, so a revoke on this pod is never undone by a stale read.
const LOCAL_REVOKE_MS = 10_000;

interface Stamped<T> {
  at: number;
  value: T;
}

export interface LocalRevoke {
  key: string;
  epoch: number;
}

// A clock stepped backwards must not stretch a cached read past its window.
function readFresh(entry: Stamped<unknown>): boolean {
  const age = Date.now() - entry.at;
  return age >= 0 && age < READ_CACHE_MS;
}

// A clock stepped backwards may only lengthen a local revoke.
function revokeLive(entry: Stamped<unknown>): boolean {
  return Date.now() - entry.at < LOCAL_REVOKE_MS;
}

function prune<T>(map: Map<string, Stamped<T>>, keep: (entry: Stamped<T>) => boolean): void {
  for (const [key, entry] of map) {
    if (keep(entry) && map.size <= READ_CACHE_MAX) return;
    map.delete(key);
  }
}

export class RevocationCache {
  private readonly reads = new Map<string, Stamped<Promise<RevocationRead>>>();
  private readonly revokes = new Map<string, Stamped<number>>();

  read(keys: RevocationKeys, load: (keys: RevocationKeys) => Promise<RevocationRead>): Promise<RevocationRead> {
    prune(this.reads, readFresh);
    const key = `${keys.userId}\u0000${keys.sid ?? ''}`;
    const hit = this.reads.get(key);
    if (hit && readFresh(hit)) return hit.value;
    const pending = load(keys);
    this.reads.delete(key);
    this.reads.set(key, { at: Date.now(), value: pending });
    pending.catch(() => {
      if (this.reads.get(key)?.value === pending) this.reads.delete(key);
    });
    return pending;
  }

  remember(revoke: LocalRevoke): void {
    prune(this.revokes, revokeLive);
    this.revokes.delete(revoke.key);
    this.revokes.set(revoke.key, { at: Date.now(), value: revoke.epoch });
  }

  revokedHere(claims: RevocationClaims): boolean {
    const keys = claims.sid ? [`sid:${claims.sid}`, `all:${claims.userId}`] : [`all:${claims.userId}`];
    return keys.some((key) => {
      const entry = this.revokes.get(key);
      return entry !== undefined && revokeLive(entry) && claims.iat < entry.value;
    });
  }

  clear(): void {
    this.reads.clear();
    this.revokes.clear();
  }
}
