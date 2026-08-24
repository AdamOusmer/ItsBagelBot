// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Fetch definitions + sealed keys store: the dashboard side of the commands
// service's urlfetch verbs (Phase 1 of docs/urlfetch/IMPLEMENTATION.md).
//
// Deliberately a thin RPC twin, NOT the fabric-hybrid read commands-store.ts
// builds: until the Go projection carries defs (the `fetch:<name>` hash fields
// beside `command:<name>`), a dashboard L1 entry would sit outside the SCOPES
// invalidation map and could serve a stale list for the full projected-policy
// window after another pod's rename/delete. The page is low-traffic and every
// action re-reads anyway, so each call here is one honest RPC.
//
// Key values are write-only: fetch_set_key seals and replies {last4} once; no
// verb in this file ever receives or returns key material.

import { rpc } from '@bagel/shared/server/nats';
import { SUB } from './services';

/** One stored definition. `json_path` is segment-array wire form (Go stores
 * the same array); dotted token spelling is a display concern built with
 * buildJsonPath(). */
export interface FetchDefView {
  name: string;
  url: string;
  json_path: string[];
  is_active: boolean;
  /** Label of the FetchKey used; '' = keyless. Dangling labels fail closed. */
  key_label: string;
}

/** label + last4 only — the plaintext never comes back over any verb. */
export interface FetchKeyView {
  label: string;
  last4: string;
  created_at: string;
}

interface FetchListReply {
  defs?: FetchDefView[];
  keys?: FetchKeyView[];
  error?: string;
}

export async function listFetches(userId: string): Promise<{ defs: FetchDefView[]; keys: FetchKeyView[] }> {
  const r = await rpc<FetchListReply>(`${SUB.commands}.fetch_list`, { user_id: userId });
  if (r.error) throw new Error(r.error);
  return {
    defs: Array.isArray(r.defs) ? r.defs : [],
    keys: Array.isArray(r.keys) ? r.keys : []
  };
}

export interface FetchDefInput {
  name: string;
  url: string;
  jsonPath: string[];
  isActive: boolean;
  keyLabel: string;
  // originalName, when set and different from name, renames in place — the
  // upsert shape of dashboard.go's command rename (single row update, not
  // delete-old + create-new).
  originalName?: string;
}

export async function upsertFetchDef(
  userId: string,
  def: FetchDefInput
): Promise<{ defs: FetchDefView[]; keys: FetchKeyView[] }> {
  await rpc(`${SUB.commands}.fetch_set_def`, {
    user_id: userId,
    name: def.name,
    url: def.url,
    json_path: def.jsonPath,
    is_active: def.isActive,
    key_label: def.keyLabel,
    original_name: def.originalName ?? ''
  });
  try {
    return await listFetches(userId);
  } catch {
    // Write landed but the read-back failed: the operation still succeeded.
    // The caller reconciles from its own draft (commands-store precedent).
    return { defs: [], keys: [] };
  }
}

// setFetchKey seals one value under a label. Rotation is the same verb:
// re-entering a value against an existing label re-seals it. The reply's
// last4 is derived at seal time so later lists never decrypt.
export interface FetchKeyEntry {
  userId: string;
  label: string;
  value: string;
}

export async function setFetchKey(key: FetchKeyEntry): Promise<string> {
  const r = await rpc<{ last4?: string; error?: string }>(`${SUB.commands}.fetch_set_key`, {
    user_id: key.userId,
    label: key.label,
    value: key.value
  });
  if (r.error) throw new Error(r.error);
  return r.last4 ?? '';
}

// One delete verb on the service covers both kinds (fetch_delete): definition
// deletes are refused while any command response still references
// `{urlfetch:<name>}` unless forced; key deletes always succeed (dangling
// key_labels fail closed until relinked).
export type FetchDeleteKind = 'def' | 'key';

interface FetchDeleteRef {
  userId: string;
  kind: FetchDeleteKind;
  name: string;
}

async function deleteFetch(ref: FetchDeleteRef): Promise<void> {
  const r = await rpc<{ error?: string }>(`${SUB.commands}.fetch_delete`, {
    user_id: ref.userId,
    kind: ref.kind,
    name: ref.name
  });
  if (r.error) throw new Error(r.error);
}

export function deleteFetchDef(ref: { userId: string; name: string }): Promise<void> {
  return deleteFetch({ userId: ref.userId, kind: 'def', name: ref.name });
}

export function deleteFetchKey(ref: { userId: string; label: string }): Promise<void> {
  return deleteFetch({ userId: ref.userId, kind: 'key', name: ref.label });
}

// --- rehearsal dry-run ------------------------------------------------------

export type FetchTestStatus = 'ok' | 'denied' | 'limited' | 'upstream_error' | 'timeout' | 'bad_def';

const FETCH_TEST_STATUSES: readonly FetchTestStatus[] = [
  'ok',
  'denied',
  'limited',
  'upstream_error',
  'timeout',
  'bad_def'
];

export interface FetchTestReply {
  status: FetchTestStatus;
  values: string[];
  ms: number;
}

// Just over gossip's custom.fetch budget so this RPC never abandons a fetch
// gossip is still completing (govee listDevices' 9s-over-8s reasoning, scaled
// to this endpoint's declared window).
const FETCH_TEST_TIMEOUT_MS = 8000;

// rehearseFetch posts the REAL chat-path request with DryRun+Fresh: same
// subject (bagel.rpc.gossip.custom.fetch), same SSRF gate, same buckets —
// dry_run only skips the emit and the bucket spend, fresh skips the positive
// cache read so authors see live data.
//
// Envelope note: the task spec names the gossiprpc.Request fields PascalCase
// (DefID/ChannelID/UserID/IsPremium/DryRun/Fresh), which differs from the
// snake_case json-tagged payloads every other dashboard RPC sends. We send
// exactly those names (they must match the Go struct's marshal form) but PARSE
// the reply tolerantly across both conventions, since the Go lane lands in
// parallel.
/** A rehearsal draft: a definition minus its activation flag. */
export type FetchDraft = Omit<FetchDefInput, 'isActive'>;

/** The gossip reply's wire shape across both casing conventions. */
interface RawRehearsalReply {
  Status?: string;
  status?: string;
  Values?: string[];
  values?: string[];
  MS?: number;
  ms?: number;
  error?: string;
}

export async function rehearseFetch(userId: string, def: FetchDraft): Promise<FetchTestReply> {
  const r = await rpc<RawRehearsalReply>(
    `${SUB.gossip}.custom.fetch`,
    {
      DefID: '',
      Def: { name: def.name, url: def.url, json_path: def.jsonPath, key_label: def.keyLabel },
      ChannelID: '',
      UserID: userId,
      IsPremium: false,
      DryRun: true,
      Fresh: true
    },
    FETCH_TEST_TIMEOUT_MS
  );

  return parsedRehearsalReply(r);
}

function parsedRehearsalReply(r: RawRehearsalReply): FetchTestReply {
  const raw = (r.status ?? r.Status ?? '').toLowerCase();
  const status: FetchTestStatus = (FETCH_TEST_STATUSES as readonly string[]).includes(raw)
    ? (raw as FetchTestStatus)
    : 'upstream_error';
  const values = r.values ?? r.Values ?? [];
  return { status, values: Array.isArray(values) ? values.map(String) : [], ms: r.ms ?? r.MS ?? 0 };
}
