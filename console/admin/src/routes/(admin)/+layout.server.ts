import type { LayoutServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { requireAdmin, isDemo } from '$lib/server/access';
import { notificationsList } from '$lib/server/services';
import { sampleNotifications } from '$lib/server/sample';

const BELL_PEEK = 5;

// Authorization gate for the whole admin group. The tailnet limits who can
// reach this host; the allowlist (auth.check) limits who can act. A request
// without an admin session is bounced to the Twitch sign-in.
export const load: LayoutServerLoad = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');

  // Best-effort peek at the most recently sent notifications for the bell.
  // There's no per-admin read state (that's a recipient concept, not an
  // operator one) so this is just "what went out recently."
  const recentNotifications = isDemo()
    ? sampleNotifications.slice(0, BELL_PEEK)
    : await notificationsList(1)
        .then((r) => r.notifications.slice(0, BELL_PEEK))
        .catch(() => []);

  return {
    id: admin.id,
    displayName: admin.display_name,
    login: admin.login,
    role: admin.role,
    recentNotifications
  };
};
