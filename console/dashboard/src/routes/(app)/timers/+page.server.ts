import type { Actions, PageServerLoad } from './$types';
import type { TimerDef } from '@bagel/shared';
import { blankTimer } from '@bagel/shared';
import {
  readTimers,
  createTimer,
  updateTimer,
  deleteTimer,
  setTimersEnabled,
  type TimerResult
} from '$lib/server/timers-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// A delegate needs the 'timers' section; a normal login always may.
function gate(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('timers')) {
    throw redirect(302, '/');
  }
}

// Demo timers so the tab renders without a live backend.
function demoTimers(): TimerDef[] {
  return [
    { ...blankTimer(), id: 'demo-1', message: 'Follow on socials: twitch.tv/yourchannel', intervalSeconds: 900 },
    { ...blankTimer(), id: 'demo-2', message: '!discord for the community server', intervalSeconds: 1800 }
  ];
}

export const load: PageServerLoad = async ({ locals }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  if (env.DEMO === '1') return { enabled: true, timers: demoTimers() };
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

export const actions: Actions = {
  create: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const draft = parseTimer(String(f.get('timer') ?? ''));
    if (!draft) return fail(400, { ok: false, error: 'Invalid timer.' });
    if (env.DEMO === '1') return { ok: true };

    let res: TimerResult;
    try {
      res = await createTimer(uid, draft);
    } catch (e) {
      console.error('[timers] create failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'create failed' });
    }
    if (!res.ok) return fail(400, { ok: false, error: res.error ?? 'failed' });
    auditDashboardImpersonation(locals.session, 'timers:create', draft.message);
    return { ok: true };
  },

  update: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const draft = parseTimer(String(f.get('timer') ?? ''));
    if (!draft || !draft.id) return fail(400, { ok: false, error: 'Invalid timer.' });
    if (env.DEMO === '1') return { ok: true };

    let res: TimerResult;
    try {
      res = await updateTimer(uid, draft);
    } catch (e) {
      console.error('[timers] update failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'update failed' });
    }
    if (!res.ok) return fail(400, { ok: false, error: res.error ?? 'failed' });
    auditDashboardImpersonation(locals.session, 'timers:update', draft.message);
    return { ok: true };
  },

  delete: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const id = String(f.get('id') ?? '');
    if (!id) return fail(400, { ok: false, error: 'Missing timer id.' });
    if (env.DEMO === '1') return { ok: true };

    let res: TimerResult;
    try {
      res = await deleteTimer(uid, id);
    } catch (e) {
      console.error('[timers] delete failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'delete failed' });
    }
    if (!res.ok) return fail(400, { ok: false, error: res.error ?? 'failed' });
    auditDashboardImpersonation(locals.session, 'timers:delete', id);
    return { ok: true };
  },

  // Master on/off for whether sesame arms any timer at all.
  toggle: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const enabled = f.get('is_enabled') === 'on';
    if (env.DEMO === '1') return { ok: true, enabled };

    try {
      await setTimersEnabled(uid, enabled);
    } catch (e) {
      console.error('[timers] toggle failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'timers:toggle', String(enabled));
    return { ok: true, enabled };
  }
};
