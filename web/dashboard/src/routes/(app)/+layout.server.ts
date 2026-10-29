// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import type { LayoutServerLoad } from './$types';
import { DASHBOARD_SECTIONS, moduleSectionLinks } from '@bagel/kit/nav';
import { translate } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import type { Session } from '#lib/server/session';
import { effectiveId } from '#lib/server/board';
import { accountState, notificationsForUser, delegationAccess, type AccountState, type NotificationWire } from '#lib/server/services';
import { DEFAULT_MODULE_FLAGS, DEMO_MODULE_FLAGS, moduleFlags } from '#lib/server/module-flags';

const DEMO = dev && env.DEMO === '1';

const BELL_PEEK = 5;

async function loadBellPeek(s: Session): Promise<{ unreadCount: number; notifications: NotificationWire[] }> {
  const cap = (list: NotificationWire[]): NotificationWire[] => list.slice(0, BELL_PEEK);
  if (DEMO) {
    const { demoNotifications } = await import('#lib/server/demo-data');
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
  if (DEMO) return (await import('#lib/server/demo-data')).demoAuthorizedDashboards;
  if (s.delegate_of) return [];
  const listed = delegationAccess(s.user_id).then((grants) =>
    grants.map((g) => ({ href: `/delegate/enter?owner=${g.owner_user_id}`, name: g.owner_login }))
  );
  return bestEffort(listed, []);
}

async function loadAccountState(locals: App.Locals, s: Session): Promise<AccountState | null> {
  if (DEMO) return (await import('#lib/server/demo-data')).demoAccountState;
  const gateRead = locals.accountState;
  if (!gateRead) return bestEffort<AccountState | null>(accountState(s.user_id), null);
  return 'value' in gateRead ? gateRead.value : null;
}

async function loadModuleFlags(s: Session): Promise<Record<string, boolean>> {
  if (DEMO) return DEMO_MODULE_FLAGS;
  return bestEffort(moduleFlags(s.user_id), DEFAULT_MODULE_FLAGS);
}

async function requireSession(locals: App.Locals, url: URL): Promise<Session> {
  const s = locals.session ?? (DEMO ? (await import('#lib/server/demo-data')).demoSession() : null);
  if (s) return s;
  const next = url.pathname + url.search;
  throw redirect(302, next === '/' ? '/login' : `/login?next=${encodeURIComponent(next)}`);
}

function delegateView(s: Session) {
  if (!s.delegate_of) return { delegateOf: s.delegate_of, delegateLogin: undefined, sections: undefined };
  return { delegateOf: s.delegate_of, delegateLogin: s.delegate_login, sections: s.sections ?? [] };
}

function isPremiumAccount(acc: AccountState | null): boolean {
  return acc?.status === 'vip' || acc?.status === 'paid';
}

export const load: LayoutServerLoad = async ({ locals, url }) => {
  const s = await requireSession(locals, url);

  const [acc, authorizedDashboards, flags] = await Promise.all([
    loadAccountState(locals, s),
    loadAuthorizedDashboards(s),
    loadModuleFlags(s)
  ]);

  return {
    moduleNav: moduleSectionLinks((key) => translate(locals.locale, key)),
    navLabels: Object.fromEntries(DASHBOARD_SECTIONS.map((def) => [def.labelKey, translate(locals.locale, def.labelKey)])),
    role: s.role,
    displayName: s.display_name,
    login: s.login,
    impersonatorLogin: s.impersonator_id ? s.impersonator_login : undefined,
    board: effectiveId(s),
    ...delegateView(s),
    bell: loadBellPeek(s),
    authorizedDashboards,
    isPremium: isPremiumAccount(acc),
    onboarded: acc?.onboarded ?? false,
    moduleFlags: flags
  };
};
