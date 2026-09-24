// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import {
  delegationList,
  delegationAccess,
  delegationCreate,
  delegationUpdate,
  delegationOptOut,
  delegationRevoke,
  deleteSelf,
  publishEventSub,
  auditDashboardImpersonation,
  notificationsForUser,
  notificationMarkRead,
  notificationMarkPeeked,
  userLocale,
  userCommandsPage,
  setCommandsPage,
  accountState,
  type NotificationWire
} from '$lib/server/services';
import { deleteFetchKey, listFetches, setFetchKey, type FetchKeyView } from '$lib/server/fetches-store';
import { purgeEdge } from '$lib/server/edge-purge';
import { commandsHref } from '@bagel/kit/site-links';
import { KEY_VALUE_MAX, slugifyName, translate, type Locale } from '@bagel/kit';
import { ACCOUNT_DELETED_COOKIE, COOKIE, SESSION_TTL_SECONDS, type Session } from '$lib/server/session';
import { revokeAllForUser, revokeSession } from '@bagel/kit/server/session-revocation';
import { isLocale, DEFAULT_LOCALE } from '@bagel/kit/i18n';
import { actionError } from '$lib/server/action-errors';
import { GRANTABLE_SECTIONS } from '@bagel/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

function tokenLabel(token: string): string {
  return token.length <= 8 ? 'token=redacted' : `token=${token.slice(0, 8)}...`;
}

function ownerSession(s: Session | null): s is Session {
  return !!s && !s.delegate_of && !s.impersonator_id;
}

async function purgeCommandsPage(userId: string): Promise<boolean> {
  const account = await accountState(userId).catch(() => null);
  if (!account?.username) return false;
  return purgeEdge([commandsHref(account.username.toLowerCase())]);
}

function ownerAction<R>(
  failMsg: string,
  run: (s: Session, form: FormData, locale: Locale) => Promise<R>
) {
  return async ({ request, locals }: { request: Request; locals: App.Locals }) => {
    const s = locals.session;
    if (!s || s.delegate_of) return fail(403, { error: actionError(locals.locale, 'Not allowed.') });

    const form = await request.formData();
    try {
      return await run(s, form, locals.locale);
    } catch {
      return fail(502, { error: actionError(locals.locale, failMsg) });
    }
  };
}

function readFetchKeys(result: PromiseSettledResult<{ defs: { name: string; key_label: string }[]; keys: FetchKeyView[] }>): {
  fetchKeys: FetchKeyView[];
  fetchKeyRefs: Record<string, string[]>;
} {
  if (result.status !== 'fulfilled') return { fetchKeys: [], fetchKeyRefs: {} };
  const fetchKeyRefs: Record<string, string[]> = {};
  for (const def of result.value.defs) {
    if (!def.key_label) continue;
    (fetchKeyRefs[def.key_label] ??= []).push(def.name);
  }
  return { fetchKeys: result.value.keys, fetchKeyRefs };
}

export const load: PageServerLoad = async ({ locals }) => {
  if (DEMO) {
    const d = await import('$lib/server/demo-data');
    return {
      given: d.demoDelegationGiven,
      received: d.demoDelegationReceived,
      grantableSections: [...GRANTABLE_SECTIONS],
      notifications: d.demoNotifications,
      savedLocale: d.demoSavedLocale,
      commandsPage: true,
      degraded: false,
      fetchKeys: d.demoFetches().keys,
      fetchKeyRefs: { weather_api: ['weather'] }
    };
  }

  const s = locals.session;
  if (!s || s.delegate_of) throw redirect(302, '/');

  const self = s.user_id;
  const [givenResult, receivedResult, notifResult, localeResult, commandsPageResult, fetchKeyResult] = await Promise.allSettled([
    delegationList(self),
    delegationAccess(self),
    notificationsForUser(self),
    userLocale(self),
    userCommandsPage(self),
    listFetches(self)
  ]);

  const notifications: NotificationWire[] = settledOr(notifResult, { notifications: [], unreadCount: 0 }).notifications;

  return {
    given: settledOr(givenResult, []),
    received: settledOr(receivedResult, []),
    grantableSections: [...GRANTABLE_SECTIONS],
    notifications,
    savedLocale: savedLocaleOf(localeResult),
    commandsPage: settledOr(commandsPageResult, true),
    degraded: [givenResult, receivedResult, localeResult, commandsPageResult].some(rejected),
    ...readFetchKeys(fetchKeyResult)
  };
};

function settledOr<T>(r: PromiseSettledResult<T>, fallback: T): T {
  return r.status === 'fulfilled' ? r.value : fallback;
}

function rejected(r: PromiseSettledResult<unknown>): boolean {
  return r.status === 'rejected';
}

function savedLocaleOf(r: PromiseSettledResult<string>): string {
  const v = settledOr(r, DEFAULT_LOCALE);
  return isLocale(v) ? v : DEFAULT_LOCALE;
}

function keyEntryError(label: string, value: string, locale: Locale): string | null {
  if (!label) return actionError(locale, 'Label is required.');
  if (label.length > 32) return actionError(locale, 'Label must be at most 32 characters.');
  if (!value.trim()) return actionError(locale, 'Key value is required.');
  if (value.length > KEY_VALUE_MAX) return translate(locale, 'serverErrors.keyValueLength', { max: KEY_VALUE_MAX });
  return null;
}

function ownerActor(s: Session | null, locale: App.Locals['locale']): { session: Session } | { status: number; error: string } {
  if (!s) return { status: 401, error: actionError(locale, 'Not signed in.') };
  if (s.delegate_of) return { status: 403, error: actionError(locale, 'Only the account owner can do that.') };
  return { session: s };
}

async function demoKeySet(label: string, value: string) {
  const d = await import('$lib/server/demo-data');
  const current = d.demoFetches();
  return {
    ok: true,
    action: 'fetchkeyset',
    name: label,
    fetchKeys: [
      ...current.keys.filter((k) => k.label !== label),
      { label, last4: value.slice(-4).replace(/[^0-9a-f]/gi, '').padEnd(4, 'x'), created_at: new Date().toISOString() }
    ]
  };
}

async function demoKeyDelete(label: string) {
  const d = await import('$lib/server/demo-data');
  return {
    ok: true,
    action: 'fetchkeydeleted',
    name: label,
    fetchKeys: d.demoFetches().keys.filter((k) => k.label !== label)
  };
}

export const actions: Actions = {
  setfetchkey: async ({ request, locals }) => {
    const form = await request.formData();
    const label = slugifyName(String(form.get('label') ?? ''));
    const value = String(form.get('value') ?? '');

    const invalid = keyEntryError(label, value, locals.locale);
    if (invalid) return fail(400, { ok: false, error: invalid });

    if (DEMO) return demoKeySet(label, value);

    const actor = ownerActor(locals.session, locals.locale);
    if (!('session' in actor)) return fail(actor.status, { ok: false, error: actor.error });

    try {
      const last4 = await setFetchKey({ userId: actor.session.user_id, label, value });
      const fresh = await listFetches(actor.session.user_id);
      auditDashboardImpersonation(actor.session, 'fetchkey:set', label);
      return { ok: true, action: 'fetchkeyset', name: label, last4, fetchKeys: fresh.keys };
    } catch {
      return fail(502, { ok: false, error: actionError(locals.locale, 'Could not seal the key.') });
    }
  },

  delfetchkey: async ({ request, locals }) => {
    const label = slugifyName(String((await request.formData()).get('label') ?? ''));

    if (DEMO) return demoKeyDelete(label);

    const actor = ownerActor(locals.session, locals.locale);
    if (!('session' in actor)) return fail(actor.status, { ok: false, error: actor.error });

    try {
      await deleteFetchKey({ userId: actor.session.user_id, label });
      const fresh = await listFetches(actor.session.user_id);
      auditDashboardImpersonation(actor.session, 'fetchkey:delete', label);
      return { ok: true, action: 'fetchkeydeleted', name: label, fetchKeys: fresh.keys };
    } catch {
      return fail(502, { ok: false, error: actionError(locals.locale, 'Could not delete the key.') });
    }
  },

  markRead: async ({ request, locals }) => {
    const s = locals.session;
    if (DEMO) return { ok: true, action: 'read' };
    if (!s) return fail(401, { error: actionError(locals.locale, 'Not signed in.') });
    if (s.delegate_of) return fail(403, { error: actionError(locals.locale, 'Only the account owner can do that.') });

    const id = Number(String((await request.formData()).get('id') ?? ''));
    if (!Number.isFinite(id) || id <= 0) return fail(400, { error: actionError(locals.locale, 'id required') });

    try {
      await notificationMarkRead(s.user_id, id);
      return { ok: true, action: 'read' };
    } catch {
      return fail(502, { error: actionError(locals.locale, 'Could not update. Try again in a moment.') });
    }
  },

  setCommandsPage: async ({ request, locals }) => {
    const s = locals.session;
    if (DEMO) return { ok: true, action: 'commands_page', edgeDelayed: false };
    if (!ownerSession(s)) return fail(403, { error: actionError(locals.locale, 'Not allowed.') });

    const enabled = ['on', 'true'].includes(String((await request.formData()).get('enabled') ?? ''));

    try {
      await setCommandsPage(s.user_id, !enabled);
    } catch {
      return fail(502, { error: actionError(locals.locale, 'Could not update. Try again in a moment.') });
    }

    return { ok: true, action: 'commands_page', edgeDelayed: !(await purgeCommandsPage(s.user_id)) };
  },

  markAllRead: async ({ request, locals }) => {
    const s = locals.session;
    if (DEMO) return { ok: true, action: 'all_read' };
    if (!s || s.delegate_of) return fail(403, { error: actionError(locals.locale, 'Not allowed.') });

    const ids = String((await request.formData()).get('ids') ?? '')
      .split(',')
      .map(Number)
      .filter((n) => Number.isFinite(n) && n > 0);
    if (ids.length === 0) return { ok: true, action: 'all_read' };

    const settled = await Promise.allSettled(ids.map((id) => notificationMarkRead(s.user_id, id)));
    if (settled.some((r) => r.status === 'rejected')) {
      return fail(502, { error: actionError(locals.locale, 'Could not update. Try again in a moment.') });
    }
    return { ok: true, action: 'all_read' };
  },

  markPeeked: async ({ locals }) => {
    const s = locals.session;
    if (DEMO) return { ok: true, action: 'peeked' };
    if (!s || s.delegate_of) return fail(403, { error: actionError(locals.locale, 'Not allowed.') });

    try {
      await notificationMarkPeeked(s.user_id);
      return { ok: true, action: 'peeked' };
    } catch {
      return fail(502, { error: actionError(locals.locale, 'Could not update.') });
    }
  },

  delete: async ({ locals, cookies, url }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: actionError(locals.locale, 'Not signed in.') });
    if (s.delegate_of) return fail(403, { error: actionError(locals.locale, 'Not allowed.') });

    try {
      // Unenroll before the row goes away: never a deleted account with live EventSub subs.
      await publishEventSub(s.user_id, false);
      await deleteSelf(s.user_id);
      auditDashboardImpersonation(s, 'account:delete');
    } catch {
      return fail(502, { error: actionError(locals.locale, 'Could not delete account.') });
    }
    cookies.delete(COOKIE, { path: '/' });
    cookies.set(ACCOUNT_DELETED_COOKIE, '1', {
      path: '/',
      httpOnly: true,
      secure: url.protocol === 'https:',
      sameSite: 'lax',
      maxAge: 60
    });
    throw redirect(302, '/goodbye');
  },

  create: ownerAction('Could not create link.', async (s, f, locale) => {
    const sections = GRANTABLE_SECTIONS.filter((sec) => f.get(sec) === 'on');
    if (sections.length === 0) return fail(400, { error: actionError(locale, 'Pick at least one section.') });

    const token = await delegationCreate(s.user_id, s.login, sections);
    auditDashboardImpersonation(s, 'delegation:create', `sections=${sections.join(',')}`);
    return {
      ok: true,
      action: 'created',
      createdGrant: {
        token,
        sections,
        delegate_login: '',
        consumed: false
      }
    };
  }),

  updateSections: ownerAction('Could not update link.', async (s, f, locale) => {
    const token = String(f.get('token') ?? '');
    if (!token) return fail(400, { error: actionError(locale, 'Missing grant.') });
    const sections = GRANTABLE_SECTIONS.filter((sec) => f.get(sec) === 'on');
    if (sections.length === 0) return fail(400, { error: actionError(locale, 'Pick at least one section.') });

    await delegationUpdate(s.user_id, token, sections);
    auditDashboardImpersonation(s, 'delegation:update', `${tokenLabel(token)} sections=${sections.join(',')}`);
    return { ok: true, action: 'updated', updatedToken: token, updatedSections: sections };
  }),

  revoke: ownerAction('Could not revoke link.', async (s, f, locale) => {
    const token = String(f.get('token') ?? '');
    if (!token) return fail(400, { error: actionError(locale, 'Missing token.') });

    await delegationRevoke(s.user_id, token);
    auditDashboardImpersonation(s, 'delegation:revoke', tokenLabel(token));
    return { ok: true, action: 'revoked' };
  }),

  optOut: ownerAction('Could not leave dashboard.', async (s, f, locale) => {
    const ownerId = String(f.get('owner_user_id') ?? '');
    if (!ownerId) return fail(400, { error: actionError(locale, 'Missing dashboard.') });

    await delegationOptOut(s.user_id, ownerId);
    auditDashboardImpersonation(s, 'delegation:opt_out', `owner=${ownerId}`);
    return { ok: true, action: 'opted_out' };
  }),

  signOutEverywhere: async ({ locals, cookies, url }) => {
    const s = locals.session;
    if (!s || s.delegate_of) return fail(403, { error: actionError(locals.locale, 'Not allowed.') });

    const now = Math.floor(Date.now() / 1000);
    await revokeAllForUser(s.user_id, now, SESSION_TTL_SECONDS);
    if (s.sid) await revokeSession(s.sid, s.expires_at - now);

    cookies.delete(COOKIE, { path: '/', secure: url.protocol === 'https:' });
    throw redirect(303, '/login?e=revoked');
  }
};
