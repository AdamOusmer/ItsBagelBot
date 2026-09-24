// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';
import { moduleSectionLinks } from '@bagel/kit/nav';
import { bestEffort } from '@bagel/kit/server/best-effort';
import type { Session } from '$lib/server/session';
import { accountState, notificationsForUser, delegationAccess, type AccountState, type NotificationWire } from '$lib/server/services';
import { DEFAULT_MODULE_FLAGS, DEMO_MODULE_FLAGS, moduleFlags } from '$lib/server/module-flags';

const DEMO = dev && env.DEMO === '1';

const BELL_PEEK = 5;

async function loadBellPeek(s: Session): Promise<{ unreadCount: number; notifications: NotificationWire[] }> {
  const cap = (list: NotificationWire[]): NotificationWire[] => list.slice(0, BELL_PEEK);
  if (DEMO) {
    const { demoNotifications } = await import('$lib/server/demo-data');
    return {
      notifications: cap(demoNotifications),
      unreadCount: demoNotifications.filter((n) => !n.read).length
    };
  }
  if (s.delegate_of) return { unreadCount: 0, notifications: [] };

  const peek = await bestEffort(notificationsForUser(s.user_id), {
    unreadCount: 0,
    notifications: [] as NotificationWire[]
  });
  return { unreadCount: peek.unreadCount, notifications: cap(peek.notifications) };
}

async function loadAuthorizedDashboards(s: Session): Promise<{ href: string; name: string }[]> {
  if (DEMO) return (await import('$lib/server/demo-data')).demoAuthorizedDashboards;
  if (s.delegate_of) return [];
  const listed = delegationAccess(s.user_id).then((grants) =>
    grants.map((g) => ({ href: `/delegate/enter?owner=${g.owner_user_id}`, name: g.owner_login }))
  );
  return bestEffort(listed, []);
}

async function loadAccountState(locals: App.Locals, s: Session): Promise<AccountState | null> {
  if (DEMO) return (await import('$lib/server/demo-data')).demoAccountState;
  const gateRead = locals.accountState;
  if (!gateRead) return bestEffort<AccountState | null>(accountState(s.user_id), null);
  return 'value' in gateRead ? gateRead.value : null;
}

async function loadModuleFlags(s: Session): Promise<Record<string, boolean>> {
  if (DEMO) return DEMO_MODULE_FLAGS;
  return bestEffort(moduleFlags(s.user_id), DEFAULT_MODULE_FLAGS);
}

export const load: LayoutServerLoad = async ({ locals, url }) => {
  let s = locals.session;
  if (!s && DEMO) s = (await import('$lib/server/demo-data')).demoSession();
  if (!s) {
    const next = url.pathname + url.search;
    throw redirect(302, next === '/' ? '/login' : `/login?next=${encodeURIComponent(next)}`);
  }

  const [acc, authorizedDashboards, flags] = await Promise.all([
    loadAccountState(locals, s),
    loadAuthorizedDashboards(s),
    loadModuleFlags(s)
  ]);
  const isPremium = acc ? acc.status === 'vip' || acc.status === 'paid' : false;

  return {
    moduleNav: moduleSectionLinks(),
    role: s.role,
    displayName: s.display_name,
    login: s.login,
    impersonatorLogin: s.impersonator_id ? s.impersonator_login : undefined,
    delegateOf: s.delegate_of,
    delegateLogin: s.delegate_of ? s.delegate_login : undefined,
    sections: s.delegate_of ? (s.sections ?? []) : undefined,
    bell: loadBellPeek(s),
    authorizedDashboards,
    isPremium,
    onboarded: acc ? acc.onboarded : false,
    moduleFlags: flags
  };
};
