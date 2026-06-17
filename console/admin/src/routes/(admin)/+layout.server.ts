import type { LayoutServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { requireAdmin } from '$lib/server/access';

// Authorization gate for the whole admin group. The tailnet limits who can
// reach this host; the allowlist (auth.check) limits who can act. A request
// without an admin session is bounced to the Twitch sign-in.
export const load: LayoutServerLoad = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');
  return { id: admin.id, displayName: admin.display_name, login: admin.login, role: admin.role };
};
