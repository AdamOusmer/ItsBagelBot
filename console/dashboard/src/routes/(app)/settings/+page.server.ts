import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import {
  delegationList,
  delegationAccess,
  delegationCreate,
  delegationRevoke,
  deleteSelf
} from '$lib/server/rpc';
import { COOKIE } from '$lib/server/session';

const SECTIONS = ['commands'] as const;

export const load: PageServerLoad = async ({ locals }) => {
  const s = locals.session;
  // Owner-only. Delegates are confined to their sections by the layout, but
  // bounce defensively in case one ever reaches this route directly.
  if (!s || s.delegate_of) throw redirect(302, '/');

  const self = s.user_id;
  let given: Awaited<ReturnType<typeof delegationList>> = [];
  let received: Awaited<ReturnType<typeof delegationAccess>> = [];
  let degraded = false;

  try {
    given = await delegationList(self);
  } catch {
    degraded = true;
  }
  try {
    received = await delegationAccess(self);
  } catch {
    degraded = true;
  }

  return { given, received, degraded };
};

export const actions: Actions = {
  delete: async ({ locals, cookies }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of) return fail(403, { error: 'Not allowed.' });

    try {
      await deleteSelf(s.user_id);
    } catch {
      return fail(502, { error: 'Could not delete account.' });
    }
    cookies.delete(COOKIE, { path: '/' });
    throw redirect(302, '/goodbye');
  },

  create: async ({ request, locals }) => {
    const s = locals.session;
    if (!s || s.delegate_of) return fail(403, { error: 'Not allowed.' });

    const f = await request.formData();
    const sections = SECTIONS.filter((sec) => f.get(sec) === 'on');
    if (sections.length === 0) return fail(400, { error: 'Pick at least one section.' });

    try {
      await delegationCreate(s.user_id, s.login, sections);
      return { ok: true, action: 'created' };
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
      return { ok: true, action: 'revoked' };
    } catch {
      return fail(502, { error: 'Could not revoke link.' });
    }
  }
};
