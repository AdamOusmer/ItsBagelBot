// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// One computed snapshot, shared by every pod of a deployment.
//
// The cache fabric's L1 is in-process and its L2 is the Go-owned projection, so
// a value that is *computed* by the console (a ranking, an aggregate, anything
// fanned out of several RPCs) has nowhere to be shared. Three pods then answer
// three versions of it, and a reader whose requests land on different pods sees
// the number jump back and forth — which reads as a bug, because from outside
// it is one.
//
// This is the missing layer: a short-lived value in Valkey, written by whichever
// pod computed it, read by the rest. Put the fabric in front of it (a 1s window
// is enough) so a busy pod asks Valkey a few times a second rather than once per
// request:
//
//   fabric (in-process, ~1s)  ->  sharedSnapshot (Valkey, ttlMs)  ->  load()
//
// Valkey is a cache here and never the source. Unconfigured (dev, tests), slow,
// or holding a payload this build cannot parse all degrade the same way: run
// load() locally and carry on. The only thing lost is the sharing.
import { masterClient } from './valkey-master';
import { withTimeout } from './resilience';

/** Bound on each Valkey round trip; past it, computing locally is faster. */
const DEFAULT_TIMEOUT_MS = 250;

/** The two operations this needs from Valkey, so tests can stand in for it. */
export interface SnapshotClient {
  get(key: string): Promise<string | null>;
  set(key: string, value: string, mode: 'PX', ttlMs: number): Promise<unknown>;
}

let testClient: SnapshotClient | null | undefined;

/** Test seam: a stand-in client, or null for "Valkey unconfigured". Pass
 *  undefined to restore the real one. */
export function setSnapshotClientForTests(client: SnapshotClient | null | undefined): void {
  testClient = client;
}

function snapshotClient(): SnapshotClient | null {
  if (testClient !== undefined) return testClient;
  return masterClient();
}

export interface SharedSnapshotOptions<T> {
  /** Key to share under. Version it: the payload is stored, not derived, so a
   *  shape change must not be read back by an older pod mid-rollout. */
  key: string;
  /** How long one computed value serves the whole deployment. */
  ttlMs: number;
  /** The real read, run on a miss. */
  load: () => Promise<T>;
  /** Publish gate. Return false to keep a value out of the shared key — a
   *  degraded or partial answer is worth serving to one request and not worth
   *  pinning on every pod for a whole window. Defaults to always publishing. */
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
  } catch {
    // Miss, timeout, or a payload this build cannot read: compute it here.
  }

  const fresh = await load();
  if (publish && !publish(fresh)) return fresh;
  try {
    await withTimeout(client.set(key, JSON.stringify(fresh), 'PX', ttlMs), timeoutMs, `snapshot set ${key}`);
  } catch {
    // The value is still correct for this pod; the others will compute their own.
  }
  return fresh;
}
