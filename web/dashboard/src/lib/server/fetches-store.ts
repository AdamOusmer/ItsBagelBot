// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { SUB } from './services';

export interface FetchDefView {
  name: string;
  url: string;
  json_path: string[];
  is_active: boolean;
  key_label: string;
}

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
    return { defs: [], keys: [] };
  }
}

export interface FetchKeyEntry {
  userId: string;
  label: string;
  value: string;
}

export async function setFetchKey(key: FetchKeyEntry): Promise<string> {
  const r = await rpc<{ last4?: string }>(`${SUB.commands}.fetch_set_key`, {
    user_id: key.userId,
    label: key.label,
    value: key.value
  });
  return r.last4 ?? '';
}

export type FetchDeleteKind = 'def' | 'key';

interface FetchDeleteRef {
  userId: string;
  kind: FetchDeleteKind;
  name: string;
}

async function deleteFetch(ref: FetchDeleteRef): Promise<void> {
  await rpc(`${SUB.commands}.fetch_delete`, {
    user_id: ref.userId,
    kind: ref.kind,
    name: ref.name
  });
}

export function deleteFetchDef(ref: { userId: string; name: string }): Promise<void> {
  return deleteFetch({ userId: ref.userId, kind: 'def', name: ref.name });
}

export function deleteFetchKey(ref: { userId: string; label: string }): Promise<void> {
  return deleteFetch({ userId: ref.userId, kind: 'key', name: ref.label });
}

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
  sample: string;
}

// Just over gossip's custom.fetch budget, so this never abandons a fetch still completing.
const FETCH_TEST_TIMEOUT_MS = 8000;

export type FetchDraft = Omit<FetchDefInput, 'isActive'>;

interface RawRehearsalReply {
  Status?: string;
  status?: string;
  Values?: string[];
  values?: string[];
  MS?: number;
  ms?: number;
  Sample?: string;
  sample?: string;
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
  const sample = r.sample ?? r.Sample ?? '';
  return {
    status,
    values: Array.isArray(values) ? values.map(String) : [],
    ms: r.ms ?? r.MS ?? 0,
    sample: typeof sample === 'string' ? sample : ''
  };
}
