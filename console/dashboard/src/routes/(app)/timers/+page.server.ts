// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { TimerDef } from '@bagel/shared';
import {
  readTimers,
  createTimer,
  updateTimer,
  deleteTimer,
  setTimersEnabled,
  type TimerResult
} from '$lib/server/timers-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { gateModulePage } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Delegate scope comes from the timers catalog def (see module-gate.ts).
function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'timers');
}

export const load: PageServerLoad = async ({ locals }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  if (DEMO) return (await import('$lib/server/demo-data')).demoTimersView();
  try {
    const view = await readTimers(uid);
    return { enabled: view.enabled, timers: view.timers };
  } catch {
    return { enabled: false, timers: [] as TimerDef[], degraded: true };
  }
};

// clampInt coerces a form value into a bounded integer.
function clampInt(raw: unknown, min: number, max: number, dflt: number): number {
  const n = Math.trunc(Number(raw));
  if (!Number.isFinite(n)) return dflt;
  return Math.min(max, Math.max(min, n));
}

// parseTimer validates and normalizes the posted timer JSON into a full
// TimerDef. Returns null on anything malformed. The interval is clamped to
// 60s-24h here; sesame floors it again defensively at arm time.
function parseTimer(raw: string): TimerDef | null {
  let obj: Partial<TimerDef>;
  try {
    obj = JSON.parse(raw) as Partial<TimerDef>;
  } catch {
    return null;
  }
  const message = String(obj.message ?? '').trim();
  if (!message || message.length > 500) return null;

  return {
    id: String(obj.id ?? ''),
    message,
    intervalSeconds: clampInt(obj.intervalSeconds, 60, 86_400, 600),
    enabled: obj.enabled !== false
  };
}

// actionContext runs the shared prologue every timers action repeats: scope
// gate, auth check, effective board id, and form parse. DEMO runs without a
// session (each action short-circuits before the store call).
async function actionContext({ request, locals }: { request: Request; locals: App.Locals }) {
  gate(locals.session);
  if (!DEMO && !locals.session) return null;
  return { uid: effectiveId(locals.session), session: locals.session, form: await request.formData() };
}

const notSignedIn = () => fail(401, { ok: false, error: 'Not signed in.' });

export const actions: Actions = {
  create: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const { uid, form: f } = ctx;
    const draft = parseTimer(String(f.get('timer') ?? ''));
    if (!draft) return fail(400, { ok: false, error: 'Invalid timer.' });
    if (DEMO) return { ok: true };

    let res: TimerResult;
    try {
      res = await createTimer(uid, draft);
    } catch (e) {
      logger.error({ err: e }, '[timers] create failed');
      return fail(400, { ok: false, error: 'create failed' });
    }
    if (!res.ok) return fail(400, { ok: false, error: res.error ?? 'failed' });
    auditDashboardImpersonation(ctx.session, 'timers:create', draft.message);
    return { ok: true };
  },

  update: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const { uid, form: f } = ctx;
    const draft = parseTimer(String(f.get('timer') ?? ''));
    if (!draft || !draft.id) return fail(400, { ok: false, error: 'Invalid timer.' });
    if (DEMO) return { ok: true };

    let res: TimerResult;
    try {
      res = await updateTimer(uid, draft);
    } catch (e) {
      logger.error({ err: e }, '[timers] update failed');
      return fail(400, { ok: false, error: 'update failed' });
    }
    if (!res.ok) return fail(400, { ok: false, error: res.error ?? 'failed' });
    auditDashboardImpersonation(ctx.session, 'timers:update', draft.message);
    return { ok: true };
  },

  delete: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const { uid, form: f } = ctx;
    const id = String(f.get('id') ?? '');
    if (!id) return fail(400, { ok: false, error: 'Missing timer id.' });
    if (DEMO) return { ok: true };

    let res: TimerResult;
    try {
      res = await deleteTimer(uid, id);
    } catch (e) {
      logger.error({ err: e }, '[timers] delete failed');
      return fail(400, { ok: false, error: 'delete failed' });
    }
    if (!res.ok) return fail(400, { ok: false, error: res.error ?? 'failed' });
    auditDashboardImpersonation(ctx.session, 'timers:delete', id);
    return { ok: true };
  },

  // Master on/off for whether sesame arms any timer at all.
  toggle: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const { uid, form: f } = ctx;
    const enabled = f.get('is_enabled') === 'on';
    if (DEMO) return { ok: true, enabled };

    try {
      await setTimersEnabled(uid, enabled);
    } catch (e) {
      logger.error({ err: e }, '[timers] toggle failed');
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(ctx.session, 'timers:toggle', String(enabled));
    return { ok: true, enabled };
  }
};
