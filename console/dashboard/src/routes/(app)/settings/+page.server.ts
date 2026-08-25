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
  type NotificationWire
} from '$lib/server/services';
import { deleteFetchKey, listFetches, setFetchKey, type FetchKeyView } from '$lib/server/fetches-store';
import { KEY_VALUE_MAX, slugifyName } from '@bagel/shared';
import { ACCOUNT_DELETED_COOKIE, COOKIE, SESSION_TTL_SECONDS, type Session } from '$lib/server/session';
import { revokeAllForUser, revokeSession } from '@bagel/shared/server/session-revocation';
import { isLocale, DEFAULT_LOCALE } from '@bagel/shared/i18n';
// The delegatable sections are the shared registry's grant set; the "what is
// grantable and why" rationale lives on that constant in @bagel/shared/nav.
import { GRANTABLE_SECTIONS } from '@bagel/shared';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

function tokenLabel(token: string): string {
  return token.length <= 8 ? 'token=redacted' : `token=${token.slice(0, 8)}...`;
}

// ownerAction wraps the shared shape of the delegation actions: owner-only
// guard, form parse, and a 502 failure when the backing RPC is down.
function ownerAction<R>(
  failMsg: string,
  run: (s: Session, form: FormData) => Promise<R>
) {
  return async ({ request, locals }: { request: Request; locals: App.Locals }) => {
    const s = locals.session;
    if (!s || s.delegate_of) return fail(403, { error: 'Not allowed.' });

    const form = await request.formData();
    try {
      return await run(s, form);
    } catch {
      return fail(502, { error: failMsg });
    }
  };
}

// readFetchKeys shapes the API-key section out of the one list read.
//
// The keys live on this page rather than beside the commands that spend them
// because they are account-level secrets and this page is already owner-only.
// Treated like notifications: a failed read shows an empty section instead of
// flagging the whole page degraded, since every other section still works.
//
// fetchKeyRefs answers "what breaks if I delete this key" — the same read
// supplies it, so naming the affected data sources costs nothing extra.
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
  // DEMO: sample grants covering the full lifecycle (pending + consumed) so the
  // page renders and is exercisable without OAuth + NATS.
  if (DEMO) {
    const d = await import('$lib/server/demo-data');
    return {
      given: d.demoDelegationGiven,
      received: d.demoDelegationReceived,
      grantableSections: [...GRANTABLE_SECTIONS],
      notifications: d.demoNotifications,
      savedLocale: d.demoSavedLocale,
      degraded: false,
      fetchKeys: d.demoFetches().keys,
      fetchKeyRefs: { weather_api: ['weather'] }
    };
  }

  const s = locals.session;
  // Owner-only. Delegates are confined to their sections by the layout, but
  // bounce defensively in case one ever reaches this route directly.
  if (!s || s.delegate_of) throw redirect(302, '/');

  const self = s.user_id;
  let given: Awaited<ReturnType<typeof delegationList>> = [];
  let received: Awaited<ReturnType<typeof delegationAccess>> = [];
  let notifications: NotificationWire[] = [];
  let degraded = false;

  const [givenResult, receivedResult, notifResult, localeResult, fetchKeyResult] = await Promise.allSettled([
    delegationList(self),
    delegationAccess(self),
    notificationsForUser(self),
    userLocale(self),
    listFetches(self)
  ]);

  if (givenResult.status === 'fulfilled') given = givenResult.value;
  else degraded = true;
  if (receivedResult.status === 'fulfilled') received = receivedResult.value;
  else degraded = true;
  // Notifications are a nice-to-have section; a failed fetch just shows empty.
  if (notifResult.status === 'fulfilled') notifications = notifResult.value.notifications;

  const savedLocale = localeResult.status === 'fulfilled' && isLocale(localeResult.value)
    ? localeResult.value
    : DEFAULT_LOCALE;
  if (localeResult.status === 'rejected') degraded = true;

  return {
    given,
    received,
    grantableSections: [...GRANTABLE_SECTIONS],
    notifications,
    savedLocale,
    degraded,
    ...readFetchKeys(fetchKeyResult)
  };
};

// One reason per line, and the caller renders whichever comes back. The UI has
// only ever shown a single message, so a field->message map was shape the
// action carried without anyone reading it.
function keyEntryError(label: string, value: string): string | null {
  if (!label) return 'Label is required.';
  if (label.length > 32) return 'Label must be at most 32 characters.';
  if (!value.trim()) return 'Key value is required.';
  if (value.length > KEY_VALUE_MAX) return `Key value must be at most ${KEY_VALUE_MAX} characters.`;
  return null;
}

// ownerActor is the guard both key actions share: API keys are account-level
// secrets, so a delegate may spend one through a data source but never read,
// rotate or destroy it. Returns the session or the refusal to render, so each
// caller spends one branch on it instead of two.
function ownerActor(s: Session | null): { session: Session } | { status: number; error: string } {
  if (!s) return { status: 401, error: 'Not signed in.' };
  if (s.delegate_of) return { status: 403, error: 'Only the account owner can do that.' };
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
  // Seal (or rotate) an API key under a label. The value crosses here once and
  // is never logged, cached, or echoed back — the reply carries last4 only, and
  // the audit trail names the label alone.
  setfetchkey: async ({ request, locals }) => {
    const form = await request.formData();
    const label = slugifyName(String(form.get('label') ?? ''));
    const value = String(form.get('value') ?? '');

    const invalid = keyEntryError(label, value);
    if (invalid) return fail(400, { ok: false, error: invalid });

    if (DEMO) return demoKeySet(label, value);

    const actor = ownerActor(locals.session);
    if (!('session' in actor)) return fail(actor.status, { ok: false, error: actor.error });

    try {
      const last4 = await setFetchKey({ userId: actor.session.user_id, label, value });
      const fresh = await listFetches(actor.session.user_id);
      auditDashboardImpersonation(actor.session, 'fetchkey:set', label);
      return { ok: true, action: 'fetchkeyset', name: label, last4, fetchKeys: fresh.keys };
    } catch {
      return fail(502, { ok: false, error: 'Could not seal the key.' });
    }
  },

  // Key delete. Always allowed server-side: data sources bound to a dangling
  // label fail closed until relinked, which is the safe direction. No undo —
  // the sealed value is destroyed.
  delfetchkey: async ({ request, locals }) => {
    const label = slugifyName(String((await request.formData()).get('label') ?? ''));

    if (DEMO) return demoKeyDelete(label);

    const actor = ownerActor(locals.session);
    if (!('session' in actor)) return fail(actor.status, { ok: false, error: actor.error });

    try {
      await deleteFetchKey({ userId: actor.session.user_id, label });
      const fresh = await listFetches(actor.session.user_id);
      auditDashboardImpersonation(actor.session, 'fetchkey:delete', label);
      return { ok: true, action: 'fetchkeydeleted', name: label, fetchKeys: fresh.keys };
    } catch {
      return fail(502, { ok: false, error: 'Could not delete the key.' });
    }
  },

  // markRead lives here (not on a dedicated notifications page) because the
  // bell dropdown and the Settings section are the only notification surfaces.
  markRead: async ({ request, locals }) => {
    const s = locals.session;
    if (DEMO) return { ok: true, action: 'read' };
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of) return fail(403, { error: 'Only the account owner can do that.' });

    const id = Number(String((await request.formData()).get('id') ?? ''));
    if (!Number.isFinite(id) || id <= 0) return fail(400, { error: 'id required' });

    try {
      await notificationMarkRead(s.user_id, id);
      return { ok: true, action: 'read' };
    } catch {
      return fail(502, { error: 'Could not update. Try again in a moment.' });
    }
  },

  // markPeeked is the bell-dropdown-open path: soft-acknowledge everything the
  // user can see. Best-effort — a failure just leaves the badge for next time,
  // so it never surfaces an error to the glance-only bell.
  markPeeked: async ({ locals }) => {
    const s = locals.session;
    if (DEMO) return { ok: true, action: 'peeked' };
    if (!s || s.delegate_of) return fail(403, { error: 'Not allowed.' });

    try {
      await notificationMarkPeeked(s.user_id);
      return { ok: true, action: 'peeked' };
    } catch {
      return fail(502, { error: 'Could not update.' });
    }
  },

  delete: async ({ locals, cookies, url }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of) return fail(403, { error: 'Not allowed.' });

    try {
      // Unenroll before the row goes away (same ordering as disconnect): if
      // either step fails the account still exists and can retry, so no
      // failure mode leaves a deleted account with live EventSub subs.
      await publishEventSub(s.user_id, false);
      await deleteSelf(s.user_id);
      auditDashboardImpersonation(s, 'account:delete');
    } catch {
      return fail(502, { error: 'Could not delete account.' });
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

  create: ownerAction('Could not create link.', async (s, f) => {
    const sections = GRANTABLE_SECTIONS.filter((sec) => f.get(sec) === 'on');
    if (sections.length === 0) return fail(400, { error: 'Pick at least one section.' });

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

  // Re-scope an existing grant: add/remove sections in place (the delegate keeps
  // the same link, and a consumed grant's access follows on their next visit).
  updateSections: ownerAction('Could not update link.', async (s, f) => {
    const token = String(f.get('token') ?? '');
    if (!token) return fail(400, { error: 'Missing grant.' });
    const sections = GRANTABLE_SECTIONS.filter((sec) => f.get(sec) === 'on');
    if (sections.length === 0) return fail(400, { error: 'Pick at least one section.' });

    await delegationUpdate(s.user_id, token, sections);
    auditDashboardImpersonation(s, 'delegation:update', `${tokenLabel(token)} sections=${sections.join(',')}`);
    return { ok: true, action: 'updated', updatedToken: token, updatedSections: sections };
  }),

  revoke: ownerAction('Could not revoke link.', async (s, f) => {
    const token = String(f.get('token') ?? '');
    if (!token) return fail(400, { error: 'Missing token.' });

    await delegationRevoke(s.user_id, token);
    auditDashboardImpersonation(s, 'delegation:revoke', tokenLabel(token));
    return { ok: true, action: 'revoked' };
  }),

  optOut: ownerAction('Could not leave dashboard.', async (s, f) => {
    const ownerId = String(f.get('owner_user_id') ?? '');
    if (!ownerId) return fail(400, { error: 'Missing dashboard.' });

    await delegationOptOut(s.user_id, ownerId);
    auditDashboardImpersonation(s, 'delegation:opt_out', `owner=${ownerId}`);
    return { ok: true, action: 'opted_out' };
  }),

  // Kills every session the owner holds (this browser included) rather than
  // just this one cookie. Owner-only, same guard as the rest of this file's
  // actions — a delegate has no session of their own to sweep and must not
  // be able to sign the owner out from someone else's board. Not wrapped in
  // ownerAction: this action ends in a redirect + cookie wipe, not a form
  // result, so it needs cookies/url that helper doesn't hand back.
  signOutEverywhere: async ({ locals, cookies, url }) => {
    const s = locals.session;
    if (!s || s.delegate_of) return fail(403, { error: 'Not allowed.' });

    const now = Math.floor(Date.now() / 1000);
    // Both calls are best-effort and never throw (fail-open, see
    // session-revocation.ts): a Valkey blip must not trap the owner in a
    // session they just asked to leave, even if the OTHER devices don't get
    // the memo until Valkey recovers.
    await revokeAllForUser(s.user_id, now, SESSION_TTL_SECONDS);
    if (s.sid) await revokeSession(s.sid, s.expires_at - now);

    cookies.delete(COOKIE, { path: '/', secure: url.protocol === 'https:' });
    throw redirect(303, '/login?e=revoked');
  }
};
