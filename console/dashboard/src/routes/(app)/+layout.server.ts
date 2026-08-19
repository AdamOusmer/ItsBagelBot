// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';
import type { Session } from '$lib/server/session';
import { accountState, notificationsForUser, delegationAccess, type NotificationWire } from '$lib/server/services';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

const BELL_PEEK = 5;

// loadBellPeek fetches the owner's notification bell, best-effort: an RPC blip
// must never block the shell, so a failed fetch just shows an empty peek.
// Delegates have no bell.
async function loadBellPeek(s: Session): Promise<{ unreadCount: number; notifications: NotificationWire[] }> {
  if (DEMO) {
    const { demoNotifications } = await import('$lib/server/demo-data');
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
  if (DEMO) return (await import('$lib/server/demo-data')).demoAuthorizedDashboards;
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
  if (!s && DEMO) s = (await import('$lib/server/demo-data')).demoSession();
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
    DEMO
      ? import('$lib/server/demo-data').then((m) => m.demoAccountState)
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
