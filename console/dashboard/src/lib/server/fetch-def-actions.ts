// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Server-side plumbing for the data-source builder that lives inside the
// command editor: form parsing, the quota/collision pre-check, the test-run
// bucket and the rehearsal dial.
//
// Its own module because these actions are HOSTED by the commands page but are
// not about commands. Left inline they made that route file own two subjects,
// which is what its whole-file complexity was reporting.
//
// Nothing here calls fail(): SvelteKit infers ActionData from the fail() calls
// written inside an action, so these return a message (or null) and the route
// turns it into the refusal.

import { ValkeyRateLimiter } from '@bagel/shared/server/rate-limit';
import {
  DEFS_PER_BROADCASTER,
  firstError,
  normName,
  parseJsonPath,
  slugifyName,
  validateFetchDef,
  type FetchDefErrors
} from '@bagel/shared';
import { listFetches, rehearseFetch, upsertFetchDef, deleteFetchDef } from '$lib/server/fetches-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { logger } from '@bagel/shared/server/logger';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// tryRpc mirrors the route helper: a failed read degrades rather than throws,
// because a stale pre-check is better than refusing a legitimate save.
async function tryRpc<T>(action: string, call: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
  try {
    return { ok: true, value: await call() };
  } catch (err) {
    logger.error({ err }, `[commands] ${action} rpc failed`);
    return { ok: false };
  }
}

// Each test run dials a third-party host for real, so it gets its own bucket
// rather than sharing a command-save allowance: 6 back-to-back attempts, then a
// refill of one per ten seconds. Same numbers the standalone fetches page used;
// moving the UI into the command editor does not make the upstream cheaper.
const fetchTestLimiter = new ValkeyRateLimiter({ name: 'fetchtest', capacity: 6, refillPerSec: 0.1 });

export interface DefForm {
  name: string;
  url: string;
  kind: 'plain' | 'json';
  path: string[];
  keyLabel: string;
  isEdit: boolean;
  originalName: string;
  /** Edit + a distinct non-empty original slug: rename detection rides on the
   * parsed draft so validator, store call and reply all read one field. */
  renamed: boolean;
}

// parseDefForm reads the builder's submission; normalization mirrors the client
// (slugifyName) so the optimistic UI key agrees with what lands.
//
// No is_active is parsed: the pause toggle is gone from the UI, so every
// definition the builder writes is active. The field still exists in the store
// and the projection, which is why the write below hard-codes true instead of
// dropping it — a def saved without it would read back as paused and silently
// stop resolving.
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

// Courtesy pre-read ahead of the service's own synchronous enforcement (COUNT
// before insert, unique (user_id,name)): our list can be a beat stale, Go owns
// truth. Assigns onto errors.name so a collision message outranks a field error.
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

// testRunThrottle returns the refusal message, or null to proceed. Demo never
// spends the bucket: it dials nothing.
export async function testRunThrottle(uid: string): Promise<string | null> {
  if (DEMO) return null;
  const decision = await fetchTestLimiter.check(`fetchtest:${uid}`);
  if (decision.allowed) return null;
  return 'Too many test runs — each one calls the real API. Wait about 10 seconds and try again.';
}

// Fields that must be sound before we dial a third-party host for real. The
// slug is not among them: the builder fetches a sample before the author has
// named anything, so an unnamed draft is expected here and only here.
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

// The token identity falls back to the key label, then to a placeholder, so an
// unnamed draft still rehearses.
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

// runRehearsal owns the dial and its error mapping. Returns null when gossip
// did not answer; the caller turns that into the 502 so ActionData still sees
// the fail() inside the action.
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

/**
 * What a data-source action answers with. The route turns a refusal into
 * fail(status, body) and returns data as-is; keeping fail() out of here is what
 * lets SvelteKit still infer ActionData from the route file.
 */
export type FetchActionResult =
  | { ok: true; data: Record<string, unknown> }
  | { ok: false; status: number; body: Record<string, unknown> };

export async function saveFetchDef(uid: string, session: Session | null, form: FormData): Promise<FetchActionResult> {
  const def = parseDefForm(form);

  // Shared validator: the client builder runs these exact checks, so this is
  // the authoritative re-check rather than a duplicate of a different shape.
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

/**
 * The service refuses while any command response still references
 * `{urlfetch:<name>}`; the client only pre-warns.
 */
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

/**
 * Rehearsal dry-run: executes the REAL chat path (same gossip subject, SSRF
 * gate, buckets) with DryRun+Fresh and the posted draft inline as Def. Returns
 * the raw body as `sample` so the builder can render a clickable tree — that is
 * the whole point of the call for a non-technical author, who otherwise has to
 * paste a response by hand. Nothing is persisted.
 */
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
