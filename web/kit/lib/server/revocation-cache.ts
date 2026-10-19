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

function prune<T>(map: Map<string, Stamped<T>>, keep: (entry: Stamped<T>) => boolean): void {
  for (const [key, entry] of map) {
    if (keep(entry) && map.size <= READ_CACHE_MAX) return;
    map.delete(key);
  }
}

// Monotonic by default: a wall-clock step must never stretch either window.
export class RevocationCache {
  private readonly reads = new Map<string, Stamped<Promise<RevocationRead>>>();
  private readonly revokes = new Map<string, Stamped<number>>();
  private readonly readFresh = (entry: Stamped<unknown>) => this.now() - entry.at < READ_CACHE_MS;
  private readonly revokeLive = (entry: Stamped<unknown>) => this.now() - entry.at < LOCAL_REVOKE_MS;

  constructor(private readonly now: () => number = () => performance.now()) {}

  read(keys: RevocationKeys, load: (keys: RevocationKeys) => Promise<RevocationRead>): Promise<RevocationRead> {
    prune(this.reads, this.readFresh);
    const key = `${keys.userId}\u0000${keys.sid ?? ''}`;
    const hit = this.reads.get(key);
    if (hit && this.readFresh(hit)) return hit.value;
    const pending = load(keys);
    this.reads.delete(key);
    this.reads.set(key, { at: this.now(), value: pending });
    pending.catch(() => {
      if (this.reads.get(key)?.value === pending) this.reads.delete(key);
    });
    return pending;
  }

  remember(revoke: LocalRevoke): void {
    prune(this.revokes, this.revokeLive);
    this.revokes.delete(revoke.key);
    this.revokes.set(revoke.key, { at: this.now(), value: revoke.epoch });
  }

  revokedHere(claims: RevocationClaims): boolean {
    const keys = claims.sid ? [`sid:${claims.sid}`, `all:${claims.userId}`] : [`all:${claims.userId}`];
    return keys.some((key) => {
      const entry = this.revokes.get(key);
      return entry !== undefined && this.revokeLive(entry) && claims.iat < entry.value;
    });
  }

  clear(): void {
    this.reads.clear();
    this.revokes.clear();
  }
}
