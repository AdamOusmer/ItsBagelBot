import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { ACCOUNT_DELETED_COOKIE } from '$lib/server/session';

export const load: PageServerLoad = ({ locals, cookies }) => {
  const wasDeleted = cookies.get(ACCOUNT_DELETED_COOKIE) === '1';
  cookies.delete(ACCOUNT_DELETED_COOKIE, { path: '/' });

  if (locals.session) throw redirect(302, '/');
  if (!wasDeleted) throw redirect(302, '/login');

  return {};
};
