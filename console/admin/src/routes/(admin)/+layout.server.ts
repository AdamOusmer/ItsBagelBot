import { redirect } from '@sveltejs/kit';
import type { LayoutServerLoad } from './$types';
import { allowed, demoSession, isDemo } from '$lib/server/access';

// Admin gate: require a valid session that is in the ADMIN_USER_IDS allowlist.
// DEMO=1 synthesizes an allowed session so the panel renders without auth.
export const load: LayoutServerLoad = ({ locals }) => {
  let s = locals.session;
  if (!s && isDemo()) s = demoSession;
  if (!allowed(s)) throw redirect(302, '/login');
  return { displayName: s!.display_name, login: s!.login };
};
