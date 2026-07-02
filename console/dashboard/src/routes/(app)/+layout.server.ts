import { redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';
import type { Session } from '$lib/server/session';
import { isBanned, notificationsForUser, type NotificationWire } from '$lib/server/services';
import { demoNotifications } from '$lib/server/demo-notifications';

const BELL_PEEK = 5;

// Demo session lets the app render without the Twitch OAuth flow wired up yet.
// Off unless DEMO=1; production paths require a real encrypted session.
const demo: Session = {
  user_id: 'demo',
  login: 'itsmavey',
  display_name: 'Mavey',
  role: 'streamer',
  expires_at: Math.floor(Date.now() / 1000) + 3600
};

export const load: LayoutServerLoad = async ({ locals, url }) => {
  let s = locals.session;
  if (!s && env.DEMO === '1') s = demo;
  if (!s) throw redirect(302, '/login');

  // Defense in depth: bounce a session whose user was banned after sign-in.
  // isBanned fails open so an RPC blip never locks out the whole app.
  if (env.DEMO !== '1' && (await isBanned(s.user_id))) throw redirect(302, '/login?e=banned');

  // A delegate may only roam the sections they were granted. Home, account
  // (overview actions) and the share-management page are owner-only. If the
  // requested path is not under an allowed section, send them to the first one.
  if (s.delegate_of) {
    const sections = s.sections ?? [];
    const allowed = sections.map((sec) => `/${sec}`);
    const onAllowed = allowed.some((p) => url.pathname === p || url.pathname.startsWith(p + '/'));
    if (!onAllowed) {
      throw redirect(302, allowed[0] ?? '/login?e=link');
    }
  }

  // Owner-only, best-effort: an RPC blip should never block the shell from
  // rendering, so a failed fetch just shows an empty bell peek.
  let unreadCount = 0;
  let notifications: NotificationWire[] = [];
  if (env.DEMO === '1') {
    notifications = demoNotifications;
    unreadCount = demoNotifications.filter((n) => !n.read).length;
  } else if (!s.delegate_of) {
    await notificationsForUser(s.user_id)
      .then((r) => {
        notifications = r.notifications;
        unreadCount = r.unreadCount;
      })
      .catch(() => {});
  }

  return {
    role: s.role,
    displayName: s.display_name,
    login: s.login,
    impersonatorLogin: s.impersonator_id ? s.impersonator_login : undefined,
    delegateOf: s.delegate_of,
    delegateLogin: s.delegate_of ? s.delegate_login : undefined,
    sections: s.delegate_of ? (s.sections ?? []) : undefined,
    unreadCount,
    bellNotifications: notifications.slice(0, BELL_PEEK)
  };
};
