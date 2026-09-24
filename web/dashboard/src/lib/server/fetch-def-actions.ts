// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { ValkeyRateLimiter } from '@bagel/kit/server/rate-limit';
import {
  DEFS_PER_BROADCASTER,
  firstError,
  normName,
  parseJsonPath,
  slugifyName,
  validateFetchDef,
  type FetchDefErrors
} from '@bagel/kit';
import { listFetches, rehearseFetch, upsertFetchDef, deleteFetchDef } from '$lib/server/fetches-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { logger } from '@bagel/kit/server/logger';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

async function tryRpc<T>(action: string, call: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
  try {
    return { ok: true, value: await call() };
  } catch (err) {
    logger.error({ err }, `[commands] ${action} rpc failed`);
    return { ok: false };
  }
}

const fetchTestLimiter = new ValkeyRateLimiter({ name: 'fetchtest', capacity: 6, refillPerSec: 0.1 });

export interface DefForm {
  name: string;
  url: string;
  kind: 'plain' | 'json';
  path: string[];
  keyLabel: string;
  isEdit: boolean;
  originalName: string;
  renamed: boolean;
}

// Always true: a def saved without is_active reads back paused and silently stops resolving.
export function parseDefForm(f: FormData): DefForm {
  const kindRaw = String(f.get('kind') ?? 'plain');
  const pathRaw = String(f.get('path') ?? '');
  const name = slugifyName(String(f.get('name') ?? ''));
  const originalName = slugifyName(String(f.get('original_name') ?? ''));
  return {
    name,
    url: String(f.get('url') ?? '').trim(),
    kind: kindRaw === 'json' ? 'json' : 'plain',
    path: kindRaw === 'json' ? (parseJsonPath(pathRaw.trim()) ?? []) : [],
    keyLabel: slugifyName(String(f.get('key_label') ?? '')),
    isEdit: f.get('edit') === '1',
    originalName,
    renamed: f.get('edit') === '1' && originalName !== '' && originalName !== name
  };
}

export async function precheckFetchConflicts(uid: string, def: DefForm, errors: FetchDefErrors): Promise<void> {
  if (DEMO) return;
  const fresh = await tryRpc('fetch-pre-check', () => listFetches(uid));
  if (!fresh.ok) return;
  const existsElsewhere = fresh.value.defs.some((d) => d.name === def.name && d.name !== def.originalName);
  if (existsElsewhere) {
    errors.name = `A data source named "${def.name}" already exists.`;
  } else if (!fresh.value.defs.some((d) => d.name === def.name) && fresh.value.defs.length >= DEFS_PER_BROADCASTER) {
    errors.name = `At most ${DEFS_PER_BROADCASTER} data sources per channel.`;
  }
}

export async function testRunThrottle(uid: string): Promise<string | null> {
  if (DEMO) return null;
  const decision = await fetchTestLimiter.check(`fetchtest:${uid}`);
  if (decision.allowed) return null;
  return 'Too many test runs. Each one calls the real API. Wait about 10 seconds and try again.';
}

const TEST_BLOCKING_FIELDS = ['url', 'path', 'kind', 'key_label'] as const;

export function testDraftError(def: DefForm): string | null {
  const errors = validateFetchDef({
    name: def.name || 'draft',
    url: def.url,
    kind: def.kind,
    path: def.path,
    keyLabel: def.keyLabel
  });
  if (!TEST_BLOCKING_FIELDS.some((field) => errors[field])) return null;
  return firstError(errors) ?? 'Fix the highlighted fields first.';
}

function rehearsalName(def: DefForm): string {
  if (def.name) return def.name;
  const fromKey = normName(def.keyLabel);
  if (fromKey) return fromKey;
  return 'draft';
}

export async function demoTestReply() {
  const { demoFetchTestRun } = await import('$lib/server/demo-data');
  const demo = demoFetchTestRun();
  return { ok: true, action: 'fetchtested', status: 'ok', values: demo.values, ms: demo.ms, sample: demo.sample };
}

export async function runRehearsal(uid: string, def: DefForm) {
  try {
    const reply = await rehearseFetch(uid, {
      name: rehearsalName(def),
      url: def.url,
      jsonPath: def.path,
      keyLabel: def.keyLabel
    });
    return {
      ok: true,
      action: 'fetchtested',
      status: reply.status,
      values: reply.values,
      ms: reply.ms,
      sample: reply.sample
    };
  } catch (e) {
    logger.error({ err: e }, '[commands] fetch testrun failed');
    return null;
  }
}

export type FetchActionResult =
  | { ok: true; data: Record<string, unknown> }
  | { ok: false; status: number; body: Record<string, unknown> };

export async function saveFetchDef(uid: string, session: Session | null, form: FormData): Promise<FetchActionResult> {
  const def = parseDefForm(form);

  const errors: FetchDefErrors = validateFetchDef({
    name: def.name,
    url: def.url,
    kind: def.kind,
    path: def.path,
    keyLabel: def.keyLabel
  });
  await precheckFetchConflicts(uid, def, errors);
  if (Object.keys(errors).length) {
    return { ok: false, status: 400, body: { ok: false, errors, error: firstError(errors) } };
  }

  if (DEMO) return { ok: true, data: await demoSaveReply(def) };

  const res = await tryRpc('savefetch', () =>
    upsertFetchDef(uid, {
      name: def.name,
      url: def.url,
      jsonPath: def.path,
      isActive: true,
      keyLabel: def.keyLabel,
      originalName: def.renamed ? def.originalName : undefined
    })
  );
  if (!res.ok) return { ok: false, status: 400, body: { ok: false } };

  auditDashboardImpersonation(session, def.isEdit ? 'fetchdef:update' : 'fetchdef:create', def.name);
  return { ok: true, data: { ok: true, action: 'fetchsaved', name: def.name, defs: res.value.defs, keys: res.value.keys } };
}

async function demoSaveReply(def: DefForm): Promise<Record<string, unknown>> {
  const { demoFetches } = await import('$lib/server/demo-data');
  const current = demoFetches();
  const defs = current.defs.filter((d) => d.name !== def.originalName && d.name !== def.name);
  defs.push({ name: def.name, url: def.url, json_path: def.path, is_active: true, key_label: def.keyLabel });
  return { ok: true, action: 'fetchsaved', name: def.name, defs, keys: current.keys };
}

export async function removeFetchDef(uid: string, session: Session | null, form: FormData): Promise<FetchActionResult> {
  const name = slugifyName(String(form.get('name') ?? ''));

  if (DEMO) {
    const { demoFetches } = await import('$lib/server/demo-data');
    const current = demoFetches();
    return {
      ok: true,
      data: { ok: true, action: 'fetchdeleted', name, defs: current.defs.filter((d) => d.name !== name), keys: current.keys }
    };
  }

  const res = await tryRpc('deletefetch', () => deleteFetchDef({ userId: uid, name }));
  if (!res.ok) return { ok: false, status: 400, body: { ok: false } };

  auditDashboardImpersonation(session, 'fetchdef:delete', name);
  const fresh = await tryRpc('deletefetch-refresh', () => listFetches(uid));
  return {
    ok: true,
    data: {
      ok: true,
      action: 'fetchdeleted',
      name,
      defs: fresh.ok ? fresh.value.defs : [],
      keys: fresh.ok ? fresh.value.keys : []
    }
  };
}

export async function rehearseFetchDef(uid: string, form: FormData): Promise<FetchActionResult> {
  const throttled = await testRunThrottle(uid);
  if (throttled) return { ok: false, status: 429, body: { ok: false, error: throttled } };

  const def = parseDefForm(form);
  const invalid = testDraftError(def);
  if (invalid) return { ok: false, status: 400, body: { ok: false, error: invalid } };

  if (DEMO) return { ok: true, data: await demoTestReply() };

  const reply = await runRehearsal(uid, def);
  if (!reply) {
    return { ok: false, status: 502, body: { ok: false, error: 'The fetch service did not answer. Try again in a moment.' } };
  }
  return { ok: true, data: reply };
}
