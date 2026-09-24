// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from '@bagel/ui/lib/icons';
import type { NavChild, NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';
import { identity, navGroups, navItems, resolveSection, type SectionDef } from './nav-core';

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
  match: readonly string[];
  ownerOnly?: boolean;
  grant?: GrantSection;
}

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

export function sectionForPath(path: string): SectionId {
  return resolveSection(DASHBOARD_SECTIONS, path, 'overview');
}

export function dashboardNavItems(opts: {
  isDelegate: boolean;
  sections: readonly string[];
  section: SectionId;
  t?: (key: MessageKey) => string;
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

export function dashboardNavGroups(
  items: readonly NavLink[],
  t?: (key: MessageKey) => string
): NavGroupDef[] {
  return navGroups((t ?? identity)('nav.manage'), items);
}
