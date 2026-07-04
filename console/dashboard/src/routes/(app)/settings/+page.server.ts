import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import {
  delegationList,
  delegationAccess,
  delegationCreate,
  delegationOptOut,
  delegationRevoke,
  deleteSelf,
  auditDashboardImpersonation,
  notificationsForUser,
  notificationMarkRead,
  notificationMarkPeeked,
  type NotificationWire
} from '$lib/server/services';
import { ACCOUNT_DELETED_COOKIE, COOKIE, type Session } from '$lib/server/session';
import { demoNotifications } from '$lib/server/demo-notifications';
import { env } from '$env/dynamic/private';

const SECTIONS = ['commands', 'modules'] as const;

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

export const load: PageServerLoad = async ({ locals }) => {
  // DEMO: sample grants covering the full lifecycle (pending + consumed) so the
  // page renders and is exercisable without OAuth + NATS.
  if (env.DEMO === '1') {
    return {
      given: [
        { token: 'demo-pending-token-1234', sections: ['commands'], delegate_login: '', consumed: false },
        { token: 'demo-consumed-token-5678', sections: ['commands'], delegate_login: 'trusty_mod', consumed: true }
      ],
      received: [{ owner_user_id: '42', owner_login: 'ferret_king', sections: ['commands'] }],
      notifications: demoNotifications,
      degraded: false
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

  const [givenResult, receivedResult, notifResult] = await Promise.allSettled([
    delegationList(self),
    delegationAccess(self),
    notificationsForUser(self)
  ]);

  if (givenResult.status === 'fulfilled') given = givenResult.value;
  else degraded = true;
  if (receivedResult.status === 'fulfilled') received = receivedResult.value;
  else degraded = true;
  // Notifications are a nice-to-have section; a failed fetch just shows empty.
  if (notifResult.status === 'fulfilled') notifications = notifResult.value.notifications;

  return { given, received, notifications, degraded };
};

export const actions: Actions = {
  // markRead lives here (not on a dedicated notifications page) because the
  // bell dropdown and the Settings section are the only notification surfaces.
  markRead: async ({ request, locals }) => {
    const s = locals.session;
    if (env.DEMO === '1') return { ok: true, action: 'read' };
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
    if (env.DEMO === '1') return { ok: true, action: 'peeked' };
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
    const sections = SECTIONS.filter((sec) => f.get(sec) === 'on');
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
  })
};
