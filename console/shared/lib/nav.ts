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

import { MODULE_CATALOG } from './catalog';
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
 *
 * `path`/`hash` (the client location, both defaulting to '') drive only the
 * subsection active flags: module categories are hash-gated to the hub page,
 * static children match their path exactly. Parents stay section-driven, so a
 * bare call (server render, Dock) yields children that simply read inactive.
 */

// Subsection children of the registry sections. They never carry grant logic
// of their own — they are built only for parents that survived the visibility
// filter above, so they render exactly when their parent does. A child never
// repeats its parent's href: the sidebar's parent row IS the link to the hub
// page, so a self-pointing child would render the same destination twice and
// put two candidates up for aria-current.

interface NavChildDef {
  href: string;
  icon: IconName;
  labelKey: MessageKey;
}

const COMMAND_CHILDREN: readonly NavChildDef[] = [
  { href: '/commands/fetches', icon: 'link', labelKey: 'nav.fetches' }
];

const SETTINGS_CHILDREN: readonly NavChildDef[] = [
  { href: '/access', icon: 'users', labelKey: 'nav.delegates' },
  { href: '/settings/import', icon: 'globe', labelKey: 'nav.importSettings' }
];

// Category rows are presentational only; an unknown category falls back to the
// parent's icon rather than growing a lookup entry per future category.
const CATEGORY_ICONS: Record<string, IconName> = {
  'Chat Tools': 'commands',
  Community: 'users',
  Moderation: 'moderation',
  Games: 'gamepad'
};

// The exact id scheme modules/+page.svelte gives its category headings for the
// scrollspy (`cat-` + slugified category) — change either side and the other's
// links strand mid-page.
function categoryAnchor(category: string): string {
  return 'cat-' + category.toLowerCase().replace(/[^a-z0-9]+/g, '-');
}

// One child per catalog category, in first-appearance order — the same order
// the hub renders its sections in. Hidden modules contribute nothing (they are
// excluded server-side too, so no hub heading exists for their category).
// Active only on the hub itself with the matching hash: prefix matches would
// light every category at once on /modules/<id> pages.
function moduleCategoryLinks(path: string, hash: string): NavLink[] {
  const anchors = new Map<string, string>();
  for (const m of MODULE_CATALOG) {
    if (!m.hidden && !anchors.has(m.category)) anchors.set(m.category, categoryAnchor(m.category));
  }
  return [...anchors].map(([category, anchor]) => ({
    href: `/modules#${anchor}`,
    icon: CATEGORY_ICONS[category] ?? 'modules',
    label: category,
    active: path === '/modules' && hash === `#${anchor}`
  }));
}

function sectionChildren(
  def: DashboardSectionDef,
  path: string,
  hash: string,
  t: (key: MessageKey) => string
): NavLink[] | undefined {
  if (def.id === 'modules') return moduleCategoryLinks(path, hash);
  const defs = def.id === 'commands' ? COMMAND_CHILDREN : def.id === 'settings' ? SETTINGS_CHILDREN : undefined;
  return defs?.map((c) => ({ href: c.href, icon: c.icon, label: t(c.labelKey), active: path === c.href }));
}

export function dashboardNavItems(opts: {
  isDelegate: boolean;
  sections: readonly string[];
  section: SectionId;
  path?: string;
  hash?: string;
  t?: (key: MessageKey) => string;
}): NavLink[] {
  const { isDelegate, sections, section } = opts;
  const t = opts.t ?? identity;
  const path = opts.path ?? '';
  const hash = opts.hash ?? '';
  return DASHBOARD_SECTIONS.filter(
    (def) =>
      !(def.ownerOnly && isDelegate) &&
      (!isDelegate || !def.grant || sections.includes(def.grant))
  ).map((def) => ({
    href: def.href,
    icon: def.icon,
    label: t(def.labelKey),
    active: section === def.id,
    children: sectionChildren(def, path, hash, t)
  }));
}

/** The single sidebar/mobile group wrapping the dock items. */
export function dashboardNavGroups(
  items: readonly NavLink[],
  t?: (key: MessageKey) => string
): NavGroupDef[] {
  return [{ label: (t ?? identity)('nav.manage'), items: [...items] }];
}
