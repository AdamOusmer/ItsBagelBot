import type { LayoutServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireAdmin } from '$lib/server/access';
import { notificationsList, type NotificationWire } from '$lib/server/services';

const BELL_PEEK = 5;
const DEMO = dev && process.env.DEMO === '1';

// Authorization gate for the whole admin group. The tailnet limits who can
// reach this host; the allowlist (auth.check) limits who can act. A request
// without an admin session is bounced to the Twitch sign-in.
export const load: LayoutServerLoad = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');

  // Bell peek is streamed (unawaited promise): navigation never blocks on the
  // notifications RPC. There's no per-admin read state (that's a recipient
  // concept), so this is just "what went out recently."
  const recentNotifications: Promise<NotificationWire[]> = DEMO
    ? import('$lib/server/demo-data').then(({ sampleNotifications }) =>
        sampleNotifications.slice(0, BELL_PEEK)
      )
    : notificationsList(1)
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
