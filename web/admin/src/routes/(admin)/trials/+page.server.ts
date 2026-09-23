// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { allows, requireRole } from '$lib/server/access';
import { audit } from '$lib/server/audit';
import { trialAdd, trialList, trialRemove, trialSetEnabled, type TrialSnapshot } from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';
const EMPTY: TrialSnapshot = { version: 1, trials: [] };

export type TrialsBundle = { snapshot: TrialSnapshot; degraded: boolean };

export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  if (!allows(layout.role, 'trials.manage')) throw redirect(302, '/');
  const bundle: Promise<TrialsBundle> = DEMO
    ? Promise.resolve({ snapshot: EMPTY, degraded: false })
    : trialList()
        .then((snapshot) => ({ snapshot, degraded: false }))
        .catch(() => ({ snapshot: EMPTY, degraded: true }));
  return { bundle };
};

function broadcasterId(form: FormData): string | null {
  const value = String(form.get('broadcaster_id') ?? '').trim();
  return /^[1-9][0-9]{0,19}$/.test(value) ? value : null;
}

export const actions: Actions = {
  add: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'trials.manage');
    if (!admin) return fail(403, { error: 'forbidden' });
    const id = broadcasterId(await request.formData());
    if (!id) return fail(400, { error: 'Enter a numeric Twitch broadcaster ID.' });
    if (DEMO) return fail(503, { error: 'Trial observation is unavailable in demo mode.' });
    try {
      await trialAdd(id);
      audit(admin, { action: 'trial_add', target: id, ok: true });
      return { ok: true, notice: `Added ${id}. Waiting for Twitch chat subscription.` };
    } catch (e) {
      const message = (e as Error).message;
      audit(admin, { action: 'trial_add', target: id, ok: false, error: message });
      return fail(400, { error: message });
    }
  },
  remove: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'trials.manage');
    if (!admin) return fail(403, { error: 'forbidden' });
    const id = broadcasterId(await request.formData());
    if (!id) return fail(400, { error: 'Invalid broadcaster ID.' });
    if (DEMO) return fail(503, { error: 'Trial observation is unavailable in demo mode.' });
    try {
      await trialRemove(id);
      audit(admin, { action: 'trial_remove', target: id, ok: true });
      return { ok: true, notice: `Stopping trial for ${id}.` };
    } catch (e) {
      const message = (e as Error).message;
      audit(admin, { action: 'trial_remove', target: id, ok: false, error: message });
      return fail(400, { error: message });
    }
  },
  set_enabled: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'trials.manage');
    if (!admin) return fail(403, { error: 'forbidden' });
    const form = await request.formData();
    const id = broadcasterId(form);
    if (!id) return fail(400, { error: 'Invalid broadcaster ID.' });
    const rawEnabled = form.get('enabled');
    if (rawEnabled !== 'true' && rawEnabled !== 'false') return fail(400, { error: 'Invalid enabled value.' });
    const enabled = rawEnabled === 'true';
    if (DEMO) return fail(503, { error: 'Trial observation is unavailable in demo mode.' });
    try {
      await trialSetEnabled(id, enabled);
      audit(admin, { action: 'trial_set_enabled', target: id, detail: String(enabled), ok: true });
      return { ok: true, notice: `Trial ${id} turned ${enabled ? 'on' : 'off'}.` };
    } catch (e) {
      const message = (e as Error).message;
      audit(admin, { action: 'trial_set_enabled', target: id, detail: String(enabled), ok: false, error: message });
      return fail(400, { error: message });
    }
  }
};
