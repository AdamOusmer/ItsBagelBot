// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Client-safe dashboard navigation. The server supplies the seven module
// category links; importing the catalog here would ship all module definitions
// to every signed-in page just to compute their counts.
import type { IconName } from '@bagel/ui/lib/icons';
import type { NavChild, NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';
import { identity, navGroups, navItems, resolveSection, type SectionDef } from './nav-core';

/**
 * Sections an owner can delegate to another account, in offer order.
 *
 * Billing is view-only for a delegate (the money actions stay owner-only; see
 * billing/+page.server.ts). Counters ride under 'modules'; timers also ride
 * under 'commands' (see the catalog's delegateSections and module-gate.ts).
 * Discord got its own grant when it got its own section (see DASHBOARD_SECTIONS
 * below): its catalog entry now declares delegateSections: ['discord'], so a
 * pre-existing 'modules' grant no longer opens /discord, a deliberate,
 * visible narrowing, not a bug. An owner who wants a delegate back on Discord
 * re-shares with the new Discord checkbox.
 */
export const GRANTABLE_SECTIONS = ['commands', 'modules', 'discord', 'channelpoints', 'billing'] as const;
export type GrantSection = (typeof GRANTABLE_SECTIONS)[number];

export type SectionId = 'overview' | 'commands' | 'modules' | 'discord' | 'billing' | 'settings';

export interface DashboardSectionDef extends SectionDef {
  id: SectionId;
  labelKey:
    | 'nav.overview'
    | 'nav.commands'
    | 'nav.modules'
    | 'nav.discord'
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
 * Dock order IS display order (overview first, settings last) and doubles as
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
    match: ['/modules', '/counters', '/quotes', '/govee', '/channelpoints', '/timers', '/loyalty', '/songqueue'],
    grant: 'modules'
  },
  {
    id: 'discord',
    labelKey: 'nav.discord',
    icon: 'discord',
    href: '/discord',
    match: ['/discord'],
    grant: 'discord'
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

/** The dashboard registry's resolution, defaulting to its landing section. */
export function sectionForPath(path: string): SectionId {
  return resolveSection(DASHBOARD_SECTIONS, path, 'overview');
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
  /** Category links computed from the catalog on the server. */
  moduleLinks: readonly NavChild[];
}): NavLink[] {
  const { isDelegate, sections, section } = opts;
  return navItems({
    sections: DASHBOARD_SECTIONS,
    visible: (def) =>
      !(def.ownerOnly && isDelegate) &&
      (!isDelegate || !def.grant || sections.includes(def.grant)),
    active: (def) => section === def.id,
    children: (def) => (def.id === 'modules' ? [...opts.moduleLinks] : undefined),
    t: opts.t
  });
}

/** The single sidebar/mobile group wrapping the dashboard's dock items. */
export function dashboardNavGroups(
  items: readonly NavLink[],
  t?: (key: MessageKey) => string
): NavGroupDef[] {
  return navGroups((t ?? identity)('nav.manage'), items);
}
