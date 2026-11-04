// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from '@bagel/ui/lib/icons';
import type { NavChild, NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';
import { navGroups, navItems, resolveSection, type SectionDef } from './nav-core';

export const GRANTABLE_SECTIONS = ['commands', 'modules', 'discord', 'channelpoints', 'billing'] as const;
export type GrantSection = (typeof GRANTABLE_SECTIONS)[number];

export type SectionId = 'overview' | 'commands' | 'flows' | 'modules' | 'songqueue' | 'discord' | 'billing' | 'settings';

export interface DashboardSectionDef extends SectionDef {
  id: SectionId;
  labelKey:
    | 'nav.overview'
    | 'nav.commands'
    | 'nav.flows'
    | 'nav.modules'
    | 'nav.songqueue'
    | 'nav.discord'
    | 'nav.billing'
    | 'nav.settings';
  icon: IconName;
  href: string;
  match: readonly string[];
  ownerOnly?: boolean;
  grant?: GrantSection;
  /** Visible when any of these existing delegation grants opens the page. */
  grantsAny?: readonly GrantSection[];
  offline?: boolean;
  demoOnly?: boolean;
  beta?: boolean;
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
    id: 'flows',
    labelKey: 'nav.flows',
    icon: 'branch',
    href: '/flows',
    match: ['/flows'],
    ownerOnly: true,
    demoOnly: true,
    beta: true
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
    id: 'songqueue',
    labelKey: 'nav.songqueue',
    icon: 'music',
    href: '/songqueue',
    match: ['/songqueue'],
    grantsAny: ['modules', 'channelpoints']
  },
  {
    id: 'discord',
    labelKey: 'nav.discord',
    icon: 'discord',
    href: '/discord',
    match: ['/discord'],
    grant: 'discord',
    offline: true
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

type NavOpts = {
  isDelegate: boolean;
  sections: readonly string[];
  section: SectionId;
  t?: (key: MessageKey) => string;
  moduleLinks: readonly NavChild[];
  showDemoSections?: boolean;
};

function grantedTo(def: DashboardSectionDef, sections: readonly string[]): boolean {
  if (def.grantsAny) return def.grantsAny.some((grant) => sections.includes(grant));
  return !def.grant || sections.includes(def.grant);
}

function sectionVisible(def: DashboardSectionDef, opts: NavOpts): boolean {
  if (def.offline) return false;
  if (def.demoOnly && opts.showDemoSections !== true) return false;
  if (!opts.isDelegate) return true;
  return !def.ownerOnly && grantedTo(def, opts.sections);
}

export function dashboardNavItems(opts: NavOpts): NavLink[] {
  const { section } = opts;
  const betaHrefs = new Set(DASHBOARD_SECTIONS.filter((def) => def.beta).map((def) => def.href));
  const betaLabel = opts.t ? opts.t('nav.beta') : 'nav.beta';
  const links = navItems({
    sections: DASHBOARD_SECTIONS,
    visible: (def) => sectionVisible(def, opts),
    current: (def) => section === def.id,
    children: (def) => (def.id === 'modules' ? [...opts.moduleLinks] : undefined),
    t: opts.t
  });
  return links.map((link) => (betaHrefs.has(link.href) ? { ...link, count: betaLabel } : link));
}

export function dashboardNavGroups(
  items: readonly NavLink[],
  t?: (key: MessageKey) => string
): NavGroupDef[] {
  return navGroups(t ? t('nav.manage') : 'nav.manage', items);
}
