import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import { delegationList, delegationCreate, delegationRevoke } from '$lib/server/rpc';

const SECTIONS = ['commands', 'modules'] as const;

export const load: PageServerLoad = async ({ locals }) => {
  const s = locals.session;
  // Owner-only: a delegate must never manage shares.
  if (!s || s.delegate_of) throw redirect(302, '/');

  try {
    return { grants: await delegationList(s.user_id) };
  } catch {
    return { grants: [], degraded: true };
  }
};

export const actions: Actions = {
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
