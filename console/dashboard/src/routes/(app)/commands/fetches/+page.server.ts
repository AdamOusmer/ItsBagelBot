// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import {
  DEFS_PER_BROADCASTER,
  KEY_VALUE_MAX,
  firstError,
  normName,
  parseJsonPath,
  slugifyName,
  validateFetchDef,
  type CommandView,
  type FetchDefErrors
} from '@bagel/shared';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';
import { ValkeyRateLimiter } from '@bagel/shared/server/rate-limit';
import {
  deleteFetchDef,
  deleteFetchKey,
  listFetches,
  rehearseFetch,
  setFetchKey,
  upsertFetchDef
} from '$lib/server/fetches-store';
import { listCommands } from '$lib/server/commands-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Delegates scoped to the commands section get fetches too: definitions exist
// only to serve command responses (commands/+page.server.ts gateCommands).
function gateCommands(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('commands')) {
    throw redirect(302, '/');
  }
}

// tryRpc runs one store RPC, logging the real failure server-side — RpcError /
// NATS timeout messages can carry internal service detail, so they go to the
// logs, never the dashboard. The caller returns a generic fail(); the client
// shows its own localized "…failed" copy.
async function tryRpc<T>(action: string, call: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
  try {
    return { ok: true, value: await call() };
  } catch (e) {
    logger.error({ err: e }, `[fetches] ${action} failed`);
    return { ok: false };
  }
}

// Per-session budget just for test runs, tighter than hooks.server.ts's
// global write tier which already applies underneath. Each run dials a real
// third-party API through gossip's WARP lane and spends that upstream's quota,
// so this is the one read-shaped action where a bored clicker costs external
// resources: capacity 6 lets an authoring session iterate quickly, then
// refillPerSec 0.1 holds a sustained one-run-per-10s pace. Measured against
// authoring flows (edit URL → re-run cycles); rejected alternative: sharing
// the import limiter's bucket, whose 10/min ceiling neither matches this
// cadence nor keeps the two budgets independently tunable. Same Valkey-backed
// limiter, same failure posture: degraded per-pod bucket when Valkey is down,
// never a page taken down.
const fetchTestLimiter = new ValkeyRateLimiter({ name: 'fetchtest', capacity: 6, refillPerSec: 0.1 });

export const load: PageServerLoad = async ({ locals }) => {
  gateCommands(locals.session);
  const uid = effectiveId(locals.session);

  if (DEMO) {
    const { demoCommandRows, demoFetches } = await import('$lib/server/demo-data');
    return { ...demoFetches(), commands: demoCommandRows };
  }

  try {
    const [fetches, commands] = await Promise.all([listFetches(uid), listCommands(uid)]);
    return { ...fetches, commands };
  } catch {
    // Surface a degraded state rather than fabricated rows.
    return { defs: [], keys: [], commands: [] as CommandView[], degraded: true };
  }
};

interface DefForm {
  name: string;
  url: string;
  kind: 'plain' | 'json';
  path: string[];
  keyLabel: string;
  isActive: boolean;
  isEdit: boolean;
  originalName: string;
}

// parseDefForm reads the editor's submission; normalization mirrors the client
// editor (slugifyName) so the optimistic UI key agrees with what lands.
function parseDefForm(f: FormData): DefForm {
  const kindRaw = String(f.get('kind') ?? 'plain');
  const pathRaw = String(f.get('path') ?? '');
  return {
    name: slugifyName(String(f.get('name') ?? '')),
    url: String(f.get('url') ?? '').trim(),
    kind: kindRaw === 'json' ? 'json' : 'plain',
    path: kindRaw === 'json' ? (parseJsonPath(pathRaw.trim()) ?? []) : [],
    keyLabel: slugifyName(String(f.get('key_label') ?? '')),
    isActive: f.get('is_active') === 'on',
    isEdit: f.get('edit') === '1',
    originalName: slugifyName(String(f.get('original_name') ?? ''))
  };
}

function actionContext({ locals }: { locals: App.Locals }): { uid: string; session: Session | null } | null {
  gateCommands(locals.session);
  if (!DEMO && !locals.session) return null;
  return { uid: effectiveId(locals.session), session: locals.session };
}

const notSignedIn = () => fail(401, { ok: false, error: 'Not signed in.' });

export const actions: Actions = {
  save: async ({ request, locals }) => {
    const ctx = actionContext({ locals });
    if (!ctx) return notSignedIn();
    const form = await request.formData();
    const def = parseDefForm(form);

    // Shared validator: the client editor runs the exact same checks, so this
    // is the authoritative re-check (commands/+page.server.ts:232 shape).
    const errors: FetchDefErrors = validateFetchDef({
      name: def.name,
      url: def.url,
      kind: def.kind,
      path: def.path,
      keyLabel: def.keyLabel
    });

    const renamed = def.isEdit && def.originalName !== '' && def.originalName !== def.name;

    // One pre-read covers the collision and quota checks — both are courtesies
    // ahead of the service's own synchronous enforcement (COUNT before insert,
    // unique (user_id,name)); our list can be a beat stale, Go owns truth.
    if (!DEMO) {
      const fresh = await tryRpc('pre-check', () => listFetches(ctx.uid));
      if (fresh.ok) {
        const existsElsewhere = fresh.value.defs.some(
          (d) => d.name === def.name && d.name !== def.originalName
        );
        if (existsElsewhere) {
          errors.name = `A definition named "${def.name}" already exists.`;
        } else if (!fresh.value.defs.some((d) => d.name === def.name) && fresh.value.defs.length >= DEFS_PER_BROADCASTER) {
          errors.name = `At most ${DEFS_PER_BROADCASTER} definitions per channel.`;
        }
      }
    }
    if (Object.keys(errors).length) {
      return fail(400, { ok: false, errors, error: firstError(errors) });
    }

    if (DEMO) {
      const { demoFetches } = await import('$lib/server/demo-data');
      const current = demoFetches();
      const row = {
        name: def.name,
        url: def.url,
        json_path: def.path,
        is_active: def.isActive,
        key_label: def.keyLabel
      };
      const defs = current.defs.filter((d) => d.name !== def.originalName && d.name !== def.name);
      defs.push(row);
      return {
        ok: true,
        action: def.isEdit ? 'updated' : 'created',
        name: def.name,
        original: renamed ? def.originalName : undefined,
        defs
      };
    }

    const res = await tryRpc('save', () =>
      upsertFetchDef(
        ctx.uid,
        { name: def.name, url: def.url, jsonPath: def.path, isActive: def.isActive, keyLabel: def.keyLabel },
        renamed ? def.originalName : undefined
      )
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, def.isEdit ? 'fetchdef:update' : 'fetchdef:create', def.name);
    return {
      ok: true,
      action: def.isEdit ? 'updated' : 'created',
      name: def.name,
      original: renamed ? def.originalName : undefined,
      ...(res.value.defs.length ? res.value : { defs: [], keys: [] })
    };
  },

  // Definition delete. The service refuses while any command response still
  // references `{urlfetch:<name>}`; the page's dialog only pre-warns.
  delete: async ({ request, locals }) => {
    const ctx = actionContext({ locals });
    if (!ctx) return notSignedIn();
    const form = await request.formData();
    const name = slugifyName(String(form.get('name') ?? ''));

    if (DEMO) {
      const { demoFetches } = await import('$lib/server/demo-data');
      const current = demoFetches();
      return { ok: true, action: 'deleted', name, defs: current.defs.filter((d) => d.name !== name), keys: current.keys };
    }

    const res = await tryRpc('delete', () => deleteFetchDef(ctx.uid, name));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'fetchdef:delete', name);
    const fresh = await tryRpc('delete-refresh', () => listFetches(ctx.uid));
    return {
      ok: true,
      action: 'deleted',
      name,
      defs: fresh.ok ? fresh.value.defs : [],
      keys: fresh.ok ? fresh.value.keys : []
    };
  },

  // Seal (or rotate) a key under a label. The value crosses here once and is
  // never logged, cached, or echoed back — the reply carries last4 only.
  setkey: async ({ request, locals }) => {
    const ctx = actionContext({ locals });
    if (!ctx) return notSignedIn();
    const form = await request.formData();
    const label = slugifyName(String(form.get('label') ?? ''));
    const value = String(form.get('value') ?? '');

    const errors: Record<string, string> = {};
    if (!label) errors.label = 'Label is required.';
    else if (label.length > 32) errors.label = 'Label must be at most 32 characters.';
    if (!value.trim()) errors.value = 'Key value is required.';
    else if (value.length > KEY_VALUE_MAX) errors.value = `Key value must be at most ${KEY_VALUE_MAX} characters.`;
    if (Object.keys(errors).length) {
      return fail(400, { ok: false, errors, error: Object.values(errors)[0] });
    }

    if (DEMO) {
      const { demoFetches } = await import('$lib/server/demo-data');
      const current = demoFetches();
      const keys = [
        ...current.keys.filter((k) => k.label !== label),
        { label, last4: value.slice(-4).replace(/[^0-9a-f]/gi, '').padEnd(4, 'x'), created_at: new Date().toISOString() }
      ];
      return { ok: true, action: 'updated', name: label, keys, silent: true };
    }

    let last4 = '';
    const res = await tryRpc('setkey', async () => {
      last4 = await setFetchKey(ctx.uid, label, value);
      return listFetches(ctx.uid);
    });
    if (!res.ok) return fail(400, { ok: false });

    // The audit trail names the label, never any part of the value.
    auditDashboardImpersonation(ctx.session, 'fetchkey:set', label);
    return { ok: true, action: 'updated', name: label, last4, keys: res.value.keys, silent: true };
  },

  // Key delete: always allowed server-side (dangling key_labels on defs fail
  // closed until relinked). No undo — the sealed value is destroyed.
  delkey: async ({ request, locals }) => {
    const ctx = actionContext({ locals });
    if (!ctx) return notSignedIn();
    const form = await request.formData();
    const label = slugifyName(String(form.get('label') ?? ''));

    if (DEMO) {
      const { demoFetches } = await import('$lib/server/demo-data');
      const current = demoFetches();
      return { ok: true, action: 'deleted', name: label, defs: current.defs, keys: current.keys.filter((k) => k.label !== label), silent: true };
    }

    const res = await tryRpc('delkey', async () => {
      await deleteFetchKey(ctx.uid, label);
      return listFetches(ctx.uid);
    });
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'fetchkey:delete', label);
    return { ok: true, action: 'deleted', name: label, defs: res.value.defs, keys: res.value.keys, silent: true };
  },

  // Rehearsal dry-run: executes the REAL chat path (same gossip subject, SSRF
  // gate, buckets) with DryRun+Fresh, the posted draft inline as Def, and no
  // emit. Rate-limited above; nothing is persisted.
  testrun: async ({ request, locals }) => {
    const ctx = actionContext({ locals });
    if (!ctx) return notSignedIn();

    if (!DEMO) {
      const decision = await fetchTestLimiter.check(`fetchtest:${ctx.uid}`);
      if (!decision.allowed)
        return fail(429, {
          ok: false,
          error: 'Too many test runs — each one calls the real API. Wait about 10 seconds and try again.'
        });
    }

    const form = await request.formData();
    const def = parseDefForm(form);
    const errors = validateFetchDef({
      name: def.name || 'draft',
      url: def.url,
      kind: def.kind,
      path: def.path,
      keyLabel: def.keyLabel
    });
    // A draft may not have its slug yet; the token identity falls back to the
    // display name for preview purposes. Everything else must be sound —
    // this is about to dial a third-party host for real.
    if (errors.url || errors.path || errors.kind || errors.key_label) {
      return fail(400, { ok: false, error: firstError(errors) ?? 'Fix the highlighted fields first.' });
    }

    if (DEMO) {
      const { demoFetchTestRun } = await import('$lib/server/demo-data');
      return { ok: true, status: 'ok', values: demoFetchTestRun().values, ms: demoFetchTestRun().ms };
    }

    let reply;
    try {
      reply = await rehearseFetch(ctx.uid, {
        name: def.name || normName(def.keyLabel) || 'draft',
        url: def.url,
        jsonPath: def.path,
        keyLabel: def.keyLabel
      });
    } catch (e) {
      logger.error({ err: e }, '[fetches] testrun failed');
      return fail(502, { ok: false, error: 'The fetch service did not answer. Try again in a moment.' });
    }

    return { ok: true, status: reply.status, values: reply.values, ms: reply.ms };
  }
};
