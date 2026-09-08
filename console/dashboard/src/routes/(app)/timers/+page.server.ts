// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { clampInt, type TimerDef } from '@bagel/shared';
import {
  readTimers,
  createTimer,
  updateTimer,
  deleteTimer,
  setTimersEnabled,
  type TimerResult
} from '$lib/server/timers-store';
import { moduleLoad } from '$lib/server/module-page';
import { moduleAction, type ModuleMutation } from '$lib/server/module-action';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

export const load: PageServerLoad = ({ locals }) =>
  moduleLoad('timers', locals.session, {
    demo: DEMO ? async () => (await import('$lib/server/demo-data')).demoTimersView() : undefined,
    read: async (uid) => {
      const view = await readTimers(uid);
      return { enabled: view.enabled, timers: view.timers };
    },
    blank: () => ({ enabled: false, timers: [] as TimerDef[] })
  });

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

// mutate binds one POST action to the module write skeleton
// ($lib/server/module-action): delegate gate, form, demo short-circuit, error
// mapping, audit. Each verb below is only its own parse plus its store call.
function mutate(op: string, invalid: string, run: ModuleMutation) {
  return moduleAction('timers', op, run, { demo: DEMO, invalid });
}

// refused answers a store result that failed for a reason the broadcaster can
// read (sesame refused the shape, the list is full) with that reason, rather
// than the generic line a thrown error gets.
function refused(res: Extract<TimerResult, { ok: false }>) {
  return fail(400, { ok: false, error: res.error ?? 'failed' });
}

export const actions: Actions = {
  create: mutate('create', 'Invalid timer.', async (uid, f) => {
    const draft = parseTimer(String(f.get('timer') ?? ''));
    if (!draft) return null;
    const res = await createTimer(uid, draft);
    return res.ok ? draft.message : refused(res);
  }),

  update: mutate('update', 'Invalid timer.', async (uid, f) => {
    const draft = parseTimer(String(f.get('timer') ?? ''));
    if (!draft || !draft.id) return null;
    const res = await updateTimer(uid, draft);
    return res.ok ? draft.message : refused(res);
  }),

  delete: mutate('delete', 'Missing timer id.', async (uid, f) => {
    const id = String(f.get('id') ?? '');
    if (!id) return null;
    const res = await deleteTimer(uid, id);
    return res.ok ? id : refused(res);
  }),

  // Master on/off for whether sesame arms any timer at all.
  toggle: mutate('toggle', 'Invalid timer.', async (uid, f) => {
    const enabled = f.get('is_enabled') === 'on';
    await setTimersEnabled(uid, enabled);
    return String(enabled);
  })
};
