import { redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';
import type { Session } from '$lib/server/session';
import { accountState, notificationsForUser, delegationAccess, type NotificationWire } from '$lib/server/services';
import { demoNotifications } from '$lib/server/demo-notifications';

const BELL_PEEK = 5;

// Demo session lets the app render without the Twitch OAuth flow wired up yet.
// Off unless DEMO=1; production paths require a real encrypted session.
const demo: Session = {
  user_id: 'demo',
  login: 'itsmavey',
  display_name: 'Mavey',
  role: 'streamer',
  iat: Math.floor(Date.now() / 1000),
  expires_at: Math.floor(Date.now() / 1000) + 3600
};

// loadBellPeek fetches the owner's notification bell, best-effort: an RPC blip
// must never block the shell, so a failed fetch just shows an empty peek.
// Delegates have no bell.
async function loadBellPeek(s: Session): Promise<{ unreadCount: number; notifications: NotificationWire[] }> {
  if (env.DEMO === '1') {
    return {
      notifications: demoNotifications,
      unreadCount: demoNotifications.filter((n) => !n.read).length
    };
  }
  if (s.delegate_of) return { unreadCount: 0, notifications: [] };

  let unreadCount = 0;
  let notifications: NotificationWire[] = [];
  await notificationsForUser(s.user_id)
    .then((r) => {
      notifications = r.notifications;
      unreadCount = r.unreadCount;
    })
    .catch(() => {});
  return { unreadCount, notifications };
}

// loadAuthorizedDashboards lists the boards shared with this user, for the
// account-menu quick switch. Normal sessions only: /delegate/enter refuses a
// delegate session, so a delegate must exit before switching boards.
// Best-effort: an RPC blip just hides the list.
async function loadAuthorizedDashboards(s: Session): Promise<{ href: string; name: string }[]> {
  if (env.DEMO === '1') {
    return [
      { href: '/delegate/enter?owner=42', name: 'ferret_king' },
      { href: '/delegate/enter?owner=77', name: 'bagel_queen' }
    ];
  }
  if (s.delegate_of) return [];
  try {
    const grants = await delegationAccess(s.user_id);
    return grants.map((g) => ({ href: `/delegate/enter?owner=${g.owner_user_id}`, name: g.owner_login }));
  } catch {
    return [];
  }
}

// Account gates (ban / deleted account / delegation revoke / delegate scope)
// run in hooks.server.ts for every request — including form actions and API
// endpoints, which this load never covers. This load only owns the
// login-redirect UX and the shell data.
export const load: LayoutServerLoad = async ({ locals, url }) => {
  let s = locals.session;
  if (!s && env.DEMO === '1') s = demo;
  // Keep the requested path through the login flow (e.g. the pricing page
  // deep-links /billing?subscribe=1), so sign-in lands back where the visitor
  // was headed instead of on the home screen.
  if (!s) {
    const next = url.pathname + url.search;
    throw redirect(302, next === '/' ? '/login' : `/login?next=${encodeURIComponent(next)}`);
  }

  const [{ unreadCount, notifications }, authorizedDashboards, acc] = await Promise.all([
    loadBellPeek(s),
    loadAuthorizedDashboards(s),
    env.DEMO === '1'
      ? Promise.resolve({ active: true, status: 'vip', onboarded: true, creatorCode: null })
      : accountState(s.user_id).catch(() => null)
  ]);

  const isPremium = acc ? acc.status === 'vip' || acc.status === 'paid' : false;

  return {
    role: s.role,
    displayName: s.display_name,
    login: s.login,
    impersonatorLogin: s.impersonator_id ? s.impersonator_login : undefined,
    delegateOf: s.delegate_of,
    delegateLogin: s.delegate_of ? s.delegate_login : undefined,
    sections: s.delegate_of ? (s.sections ?? []) : undefined,
    unreadCount,
    bellNotifications: notifications.slice(0, BELL_PEEK),
    authorizedDashboards,
    isPremium,
    onboarded: acc ? acc.onboarded : false
  };
};
