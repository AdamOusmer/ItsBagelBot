import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import {
  delegationList,
  delegationAccess,
  delegationCreate,
  delegationOptOut,
  delegationRevoke,
  deleteSelf,
  auditDashboardImpersonation
} from '$lib/server/services';
import { ACCOUNT_DELETED_COOKIE, COOKIE } from '$lib/server/session';
import { env } from '$env/dynamic/private';

const SECTIONS = ['commands'] as const;

function tokenLabel(token: string): string {
  return token.length <= 8 ? 'token=redacted' : `token=${token.slice(0, 8)}...`;
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
  let degraded = false;

  const [givenResult, receivedResult] = await Promise.allSettled([
    delegationList(self),
    delegationAccess(self)
  ]);

  if (givenResult.status === 'fulfilled') given = givenResult.value;
  else degraded = true;
  if (receivedResult.status === 'fulfilled') received = receivedResult.value;
  else degraded = true;

  return { given, received, degraded };
};

export const actions: Actions = {
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

  create: async ({ request, locals }) => {
    const s = locals.session;
    if (!s || s.delegate_of) return fail(403, { error: 'Not allowed.' });

    const f = await request.formData();
    const sections = SECTIONS.filter((sec) => f.get(sec) === 'on');
    if (sections.length === 0) return fail(400, { error: 'Pick at least one section.' });

    try {
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
    } catch {
      return fail(502, { error: 'Could not create link.' });
    }
  },

  revoke: async ({ request, locals }) => {
    const s = locals.session;
    if (!s || s.delegate_of) return fail(403, { error: 'Not allowed.' });

    const f = await request.formData();
    const token = String(f.get('token') ?? '');
    if (!token) return fail(400, { error: 'Missing token.' });

    try {
      await delegationRevoke(s.user_id, token);
      auditDashboardImpersonation(s, 'delegation:revoke', tokenLabel(token));
      return { ok: true, action: 'revoked' };
    } catch {
      return fail(502, { error: 'Could not revoke link.' });
    }
  },

  optOut: async ({ request, locals }) => {
    const s = locals.session;
    if (!s || s.delegate_of) return fail(403, { error: 'Not allowed.' });

    const f = await request.formData();
    const ownerId = String(f.get('owner_user_id') ?? '');
    if (!ownerId) return fail(400, { error: 'Missing dashboard.' });

    try {
      await delegationOptOut(s.user_id, ownerId);
      auditDashboardImpersonation(s, 'delegation:opt_out', `owner=${ownerId}`);
      return { ok: true, action: 'opted_out' };
    } catch {
      return fail(502, { error: 'Could not leave dashboard.' });
    }
  }
};
