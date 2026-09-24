// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { masterClient } from './valkey-master';
import { withTimeout } from './resilience';

const DEFAULT_TIMEOUT_MS = 250;

export interface SnapshotClient {
  get(key: string): Promise<string | null>;
  set(key: string, value: string, mode: 'PX', ttlMs: number): Promise<unknown>;
}

let testClient: SnapshotClient | null | undefined;

export function setSnapshotClientForTests(client: SnapshotClient | null | undefined): void {
  testClient = client;
}

function snapshotClient(): SnapshotClient | null {
  if (testClient !== undefined) return testClient;
  return masterClient();
}

export interface SharedSnapshotOptions<T> {
  key: string;
  ttlMs: number;
  load: () => Promise<T>;
  publish?: (value: T) => boolean;
  timeoutMs?: number;
}

export async function sharedSnapshot<T>(opts: SharedSnapshotOptions<T>): Promise<T> {
  const { key, ttlMs, load, publish, timeoutMs = DEFAULT_TIMEOUT_MS } = opts;
  const client = snapshotClient();
  if (!client) return load();

  try {
    const hit = await withTimeout<string | null>(client.get(key), timeoutMs, `snapshot get ${key}`);
    if (hit) return JSON.parse(hit) as T;
  } catch {}

  const fresh = await load();
  if (publish && !publish(fresh)) return fresh;
  try {
    await withTimeout(client.set(key, JSON.stringify(fresh), 'PX', ttlMs), timeoutMs, `snapshot set ${key}`);
  } catch {}
  return fresh;
}
