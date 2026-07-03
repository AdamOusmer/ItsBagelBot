import { redirect, type Cookies } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';
import { COOKIE, type Session } from '$lib/server/session';
import { accountState, isBanned, notificationsForUser, type NotificationWire } from '$lib/server/services';
import { RpcError } from '@bagel/shared/server/nats';
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

// enforceAccountGates bounces a session whose account was banned or deleted
// after sign-in. Both checks fail open on a transport blip so an RPC outage
// never locks the whole app out; only an authoritative "no such user"
// (RpcError) wipes the cookie.
async function enforceAccountGates(s: Session, url: URL, cookies: Cookies): Promise<void> {
  // Defense in depth: bounce a session whose user was banned after sign-in.
  if (await isBanned(s.user_id)) throw redirect(302, '/login?e=banned');

  // Ghost-session gate: a session cookie stays cryptographically valid until
  // it expires, so a browser that still holds one for a DELETED account (or
  // one deleted from another device) could keep acting on the app — and its
  // stale deletion cookies could bounce it through /goodbye on any action.
  // accountState is fabric-cached, so this costs ~0 on the hot path.
  try {
    await accountState(s.user_id);
  } catch (err) {
    if (err instanceof RpcError) {
      cookies.delete(COOKIE, { path: '/', secure: url.protocol === 'https:' });
      throw redirect(302, '/login?e=signedout');
    }
    // Transient transport problem: keep the session, pages degrade instead.
  }
}

// enforceDelegateScope keeps a delegate inside the sections they were granted.
// Home, account (overview actions) and the share-management page are
// owner-only; an out-of-scope path bounces to the first allowed section.
function enforceDelegateScope(s: Session, url: URL): void {
  if (!s.delegate_of) return;
  const allowed = (s.sections ?? []).map((sec) => `/${sec}`);
  const onAllowed = allowed.some((p) => url.pathname === p || url.pathname.startsWith(p + '/'));
  if (!onAllowed) throw redirect(302, allowed[0] ?? '/login?e=link');
}

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

export const load: LayoutServerLoad = async ({ locals, url, cookies }) => {
  let s = locals.session;
  if (!s && env.DEMO === '1') s = demo;
  // Keep the requested path through the login flow (e.g. the pricing page
  // deep-links /billing?subscribe=1), so sign-in lands back where the visitor
  // was headed instead of on the home screen.
  if (!s) {
    const next = url.pathname + url.search;
    throw redirect(302, next === '/' ? '/login' : `/login?next=${encodeURIComponent(next)}`);
  }

  if (env.DEMO !== '1') await enforceAccountGates(s, url, cookies);
  enforceDelegateScope(s, url);

  const { unreadCount, notifications } = await loadBellPeek(s);

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
