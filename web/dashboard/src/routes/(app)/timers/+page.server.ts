// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { type TimerDef } from '@bagel/kit';
import {
  readTimers,
  createTimer,
  updateTimer,
  deleteTimer,
  setTimersEnabled,
  type TimerResult
} from '$lib/server/timers-store';
import { parseTimer } from '$lib/server/timers-parse';
import { moduleLoad } from '$lib/server/module-page';
import { moduleAction, type ModuleMutation } from '$lib/server/module-action';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

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

function mutate(op: string, invalid: string, run: ModuleMutation) {
  return moduleAction('timers', op, run, { demo: DEMO, invalid });
}

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

  toggle: mutate('toggle', 'Invalid timer.', async (uid, f) => {
    const enabled = f.get('is_enabled') === 'on';
    await setTimersEnabled(uid, enabled);
    return String(enabled);
  })
};
