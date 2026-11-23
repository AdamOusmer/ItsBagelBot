// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import type { Locale } from '@bagel/kit/i18n';
import { allows, requireRole, type AdminIdentity } from '#lib/server/access';
import { adminText } from '#lib/server/admin-action';
import { audit } from '#lib/server/audit';
import { trialAdd, trialRemove, trialSetEnabled } from '#lib/server/services';
import { readTrialSnapshot, trialDemo } from './trial-snapshot.server';

type TrialAction = 'add' | 'remove' | 'set_enabled';
type TrialChange = { id: string; enabled: boolean };

const TRIAL_FAILURE_KEYS = new Map([
  ['invalid_id', 'invalidId'],
  ['invalid_request', 'invalidId'],
  ['already_registered', 'alreadyRegistered'],
  ['full', 'full'],
  ['not_found', 'notFound'],
  ['stopping', 'stopping']
]);

export const load: PageServerLoad = async ({ depends, parent }) => {
  depends('/trials/snapshot');
  const { role } = await parent();
  if (!allows(role, 'trials.manage')) throw redirect(302, '/');
  return { snapshot: readTrialSnapshot().catch(() => null) };
};

function parseChange(action: TrialAction, form: FormData) {
  const id = String(form.get('broadcaster_id') ?? '').trim();
  if (!/^[1-9][0-9]{0,19}$/.test(id)) return { error: action === 'add' ? 'idRequired' : 'invalidId' } as const;
  const enabled = form.get('enabled');
  if (action === 'set_enabled' && !validEnabledValue(enabled)) return { error: 'invalidEnabled' } as const;
  return { id, enabled: enabled === 'true' };
}

function validEnabledValue(value: FormDataEntryValue | null): boolean {
  return value === 'true' || value === 'false';
}

async function applyChange(action: TrialAction, { id, enabled }: TrialChange) {
  switch (action) {
    case 'add':
      await trialAdd(id);
      return 'added';
    case 'remove':
      await trialRemove(id);
      return 'removed';
    case 'set_enabled':
      await trialSetEnabled(id, enabled);
      return enabled ? 'enabled' : 'disabled';
  }
}

function trialFailure(locale: Locale, status: number, reason: string) {
  return fail(status, { error: adminText(locale, `admin.trials.action.${reason}`) });
}

async function recordChange(admin: AdminIdentity, locale: Locale, action: TrialAction, change: TrialChange) {
  const { id, enabled } = change;
  const entry = { action: `trial_${action}`, target: id, detail: action === 'set_enabled' ? String(enabled) : undefined };
  try {
    const notice = await applyChange(action, change);
    audit(admin, { ...entry, ok: true });
    return { ok: true, notice: adminText(locale, `admin.trials.action.${notice}`, { id }) };
  } catch (error) {
    const message = (error as Error).message;
    audit(admin, { ...entry, ok: false, error: message });
    return trialFailure(locale, 400, TRIAL_FAILURE_KEYS.get(message) ?? 'unavailable');
  }
}

function trialAction(action: TrialAction) {
  return async ({ request, locals }: RequestEvent) => {
    const { locale } = locals;
    const admin = await requireRole({ locals }, 'trials.manage');
    if (!admin) return fail(403, { error: adminText(locale, 'admin.action.forbidden') });
    const change = parseChange(action, await request.formData());
    if (change.error) return trialFailure(locale, 400, change.error);
    if (trialDemo) return trialFailure(locale, 503, 'demoUnavailable');
    return recordChange(admin, locale, action, change);
  };
}

export const actions = {
  add: trialAction('add'),
  remove: trialAction('remove'),
  set_enabled: trialAction('set_enabled')
} satisfies Actions;
