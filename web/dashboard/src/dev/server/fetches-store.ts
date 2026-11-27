// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FetchDefInput, FetchDraft, FetchKeyEntry, FetchList, FetchTestReply } from '#lib/server/fetches-store';
import { demoFetches, demoFetchTestRun } from '../fixtures';

export * from '#lib/server/fetches-store';

const state: FetchList = demoFetches();

export async function listFetches(): Promise<FetchList> {
  return structuredClone(state);
}

export async function upsertFetchDef(_userId: string, def: FetchDefInput): Promise<FetchList> {
  state.defs = state.defs.filter((d) => d.name !== def.originalName && d.name !== def.name);
  state.defs.push({ name: def.name, url: def.url, json_path: def.jsonPath, is_active: def.isActive, key_label: def.keyLabel });
  return listFetches();
}

export async function setFetchKey(key: FetchKeyEntry): Promise<string> {
  const last4 = key.value.slice(-4);
  state.keys = [...state.keys.filter((k) => k.label !== key.label), { label: key.label, last4, created_at: new Date().toISOString() }];
  return last4;
}

export async function deleteFetchDef(ref: { name: string }): Promise<void> {
  state.defs = state.defs.filter((d) => d.name !== ref.name);
}

export async function deleteFetchKey(ref: { label: string }): Promise<void> {
  state.keys = state.keys.filter((k) => k.label !== ref.label);
}

export async function rehearseFetch(_userId: string, _def: FetchDraft): Promise<FetchTestReply> {
  return { ...demoFetchTestRun(), status: 'ok' };
}
