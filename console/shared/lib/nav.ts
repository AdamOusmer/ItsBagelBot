// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The dashboard nav registry: one declarative list owning every (app) section's
// path, icon, label key, match prefixes and access shape. It exists because the
// section ladder used to be hand-maintained in four places — layout breadcrumb,
// layout nav items, guard delegate paths, settings grantable-sections — and a
// new bespoke page registered in one of them but not the others silently fell
// back to the wrong breadcrumb or vanished from the dock. Adding a page now
// means adding ONE entry here with its match prefixes; every consumer derives
// from this list, so they cannot drift.

import type { IconName } from './icons';
import type { NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';

// The label translator defaults to identity rather than pulling in
// i18n/messages: that module loads locales via Vite's import.meta.glob, which
// has no meaning outside a Vite build (bun test throws), and this module sits
// in guard.ts's boot import graph. The layout always injects its real `t`.
const identity =
  (key: MessageKey): string =>
  key;

/**
 * Sections an owner can delegate to another account, in offer order.
 *
 * Billing is view-only for a delegate (the money actions stay owner-only — see
 * billing/+page.server.ts). Counters ride under 'modules'; timers also ride
 * under 'commands' (see the catalog's delegateSections and module-gate.ts).
 */
export const GRANTABLE_SECTIONS = ['commands', 'modules', 'channelpoints', 'billing'] as const;
export type GrantSection = (typeof GRANTABLE_SECTIONS)[number];

export type SectionId = 'overview' | 'commands' | 'modules' | 'billing' | 'settings';

export interface DashboardSectionDef {
  id: SectionId;
  labelKey:
    | 'nav.overview'
    | 'nav.commands'
    | 'nav.modules'
    | 'nav.billing'
    | 'nav.settings';
  icon: IconName;
  href: string;
  /**
   * Path prefixes that resolve to this section for breadcrumbs/active state.
   * Bespoke pages (counters, quotes, govee, timers, loyalty) list the module
   * prefix they live under so they breadcrumb as Modules without their own nav
   * entry; '/' is exact-match only (it would otherwise prefix-match everything).
   */
  match: readonly string[];
  /** Hidden from delegates (Overview, Settings). */
  ownerOnly?: boolean;
  /** Visible to a delegate only when granted this section. */
  grant?: GrantSection;
}

/**
 * Dock order IS display order — overview first, settings last — and doubles as
 * the breadcrumb tiebreak. A new page joins here, nowhere else.
 */
export const DASHBOARD_SECTIONS: readonly DashboardSectionDef[] = [
  {
    id: 'overview',
    labelKey: 'nav.overview',
    icon: 'overview',
    href: '/',
    match: ['/'],
    ownerOnly: true
  },
  {
    id: 'commands',
    labelKey: 'nav.commands',
    icon: 'commands',
    href: '/commands',
    match: ['/commands'],
    grant: 'commands'
  },
  {
    id: 'modules',
    labelKey: 'nav.modules',
    icon: 'modules',
    href: '/modules',
    match: ['/modules', '/counters', '/quotes', '/govee', '/channelpoints', '/timers', '/loyalty'],
    grant: 'modules'
  },
  {
    id: 'billing',
    labelKey: 'nav.billing',
    icon: 'card',
    href: '/billing',
    match: ['/billing'],
    grant: 'billing'
  },
  {
    id: 'settings',
    labelKey: 'nav.settings',
    icon: 'settings',
    href: '/settings',
    match: ['/settings', '/access'],
    ownerOnly: true
  }
];

/**
 * Longest-prefix resolution over the registry's match lists ('/' exact-match
 * only), falling back to overview. Prefixes are disjoint today, so length and
 * declaration order can never disagree.
 */
export function sectionForPath(path: string): SectionId {
  let best: SectionId = 'overview';
  let bestLen = 0;
  for (const def of DASHBOARD_SECTIONS) {
    for (const prefix of def.match) {
      const hit = prefix === '/' ? path === '/' : path.startsWith(prefix);
      if (hit && prefix.length > bestLen) {
        best = def.id;
        bestLen = prefix.length;
      }
    }
  }
  return best;
}

/**
 * Dock items for the current viewer: owner sees everything in dock order; a
 * delegate loses ownerOnly entries and keeps only grants they hold. `t` is
 * injected (the caller's locale-bound translator); defaults to identity so pure
 * callers (tests, server) need no i18n context.
 */
export function dashboardNavItems(opts: {
  isDelegate: boolean;
  sections: readonly string[];
  section: SectionId;
  t?: (key: MessageKey) => string;
}): NavLink[] {
  const { isDelegate, sections, section } = opts;
  const t = opts.t ?? identity;
  return DASHBOARD_SECTIONS.filter(
    (def) =>
      !(def.ownerOnly && isDelegate) &&
      (!isDelegate || !def.grant || sections.includes(def.grant))
  ).map((def) => ({
    href: def.href,
    icon: def.icon,
    label: t(def.labelKey),
    active: section === def.id
  }));
}

/** The single sidebar/mobile group wrapping the dock items. */
export function dashboardNavGroups(
  items: readonly NavLink[],
  t?: (key: MessageKey) => string
): NavGroupDef[] {
  return [{ label: (t ?? identity)('nav.manage'), items: [...items] }];
}
