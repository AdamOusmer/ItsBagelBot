// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The dashboard nav registry: one declarative list owning every (app) section's
// path, icon, label key, match prefixes and access shape. It exists because the
// section ladder used to be hand-maintained in four places: layout breadcrumb,
// layout nav items, guard delegate paths, settings grantable-sections, and a
// new bespoke page registered in one of them but not the others silently fell
// back to the wrong breadcrumb or vanished from the dock. Adding a page now
// means adding ONE entry here with its match prefixes; every consumer derives
// from this list, so they cannot drift.

import type { IconName } from './icons';
import type { NavChild, NavGroupDef, NavLink } from './types';
import type { MessageKey } from './i18n/keys';
// Both are pure data/pure functions -- no import.meta.glob, no Vite-only
// entry points -- so they are safe in guard.ts's boot import graph.
import { MODULE_CATALOG, moduleDelegateSections } from './types';
import { MODULE_CATEGORY_I18N, MODULE_CATEGORY_ORDER, categoryHref } from './module-index';
// The console-agnostic half lives in nav-core so a consumer that wants only the
// generic machinery (the admin registry) does not drag MODULE_CATALOG in with
// it. Re-exported here so every existing '@bagel/shared/nav' importer is
// unchanged.
import { identity, navGroups, navItems, resolveSection, type SectionDef } from './nav-core';

export { navGroups, navItems, resolveSection } from './nav-core';
export type { SectionDef };

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
}): NavLink[] {
  const { isDelegate, sections, section } = opts;
  return navItems({
    sections: DASHBOARD_SECTIONS,
    visible: (def) =>
      !(def.ownerOnly && isDelegate) &&
      (!isDelegate || !def.grant || sections.includes(def.grant)),
    active: (def) => section === def.id,
    children: (def) => (def.id === 'modules' ? moduleSectionLinks(opts.t) : undefined),
    t: opts.t
  });
}

/**
 * The sections the /modules page is itself divided into, in the order that page
 * renders them. The rail nests these under Modules; the individual modules are
 * NOT nav entries -- a module is a tile on that page, and only the bespoke
 * href modules own a route, so listing them made the rail disagree with the
 * page it points at. Each href is the same in-page anchor categoryHref() jumps
 * to, and the count is how many modules that section holds.
 */
export function moduleSectionLinks(t?: (key: MessageKey) => string): NavChild[] {
  const label = t ?? identity;
  return MODULE_CATEGORY_ORDER.map((name) => ({
    href: `/modules${categoryHref(name)}`,
    label: label(MODULE_CATEGORY_I18N[name].label as MessageKey),
    count: MODULE_CATALOG.filter((def) => def.category === name).length
  }));
}

/** The single sidebar/mobile group wrapping the dashboard's dock items. */
export function dashboardNavGroups(
  items: readonly NavLink[],
  t?: (key: MessageKey) => string
): NavGroupDef[] {
  return navGroups((t ?? identity)('nav.manage'), items);
}

/**
 * delegateAllowedPaths lists the (app) path prefixes a delegate may open: each
 * granted section's own page, plus every bespoke module page whose catalog def
 * is opened by one of those grants (moduleDelegateSections). The read-only
 * counter name list also opens to the commands grant so commands-only delegates
 * can use the picker.
 */
export function delegateAllowedPaths(sections: readonly string[]): string[] {
  const allowed = sections
    .filter((sec) => (GRANTABLE_SECTIONS as readonly string[]).includes(sec))
    .map((sec) => `/${sec}`);
  for (const def of MODULE_CATALOG) {
    if (def.href && moduleDelegateSections(def).some((sec) => sections.includes(sec))) {
      allowed.push(def.href);
    }
  }
  if (sections.includes('commands')) allowed.push('/counters/list');
  return allowed;
}

/**
 * pathnameAllowed checks a request path against the delegate's allowed-path
 * list. An exact hit always passes; a prefix hit (a sub-route under a
 * granted section) usually does too, EXCEPT under '/modules': that prefix
 * covers the generic per-module reply page for every catalog module, but a
 * module can declare its own narrower delegateSections (channel points), so
 * admitting '/modules/<id>' on the strength of the bare 'modules' grant
 * would let it reach a module it was never granted.
 */
export function pathnameAllowed(pathname: string, allowed: string[], sections: readonly string[]): boolean {
  if (allowed.includes(pathname)) return true;
  const prefix = allowed.find((p) => pathname.startsWith(p + '/'));
  if (!prefix) return false;
  if (prefix !== '/modules') return true;
  const id = pathname.slice(prefix.length + 1).split('/')[0];
  return moduleSubpathAllowed(id, sections);
}

export function moduleSubpathAllowed(id: string, sections: readonly string[]): boolean {
  const def = MODULE_CATALOG.find((d) => d.id === id);
  if (!def) return true;
  return moduleDelegateSections(def).some((sec) => sections.includes(sec));
}
