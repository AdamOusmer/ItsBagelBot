// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Catalog-backed navigation and access rules. Browser layouts import
// nav-dashboard instead and receive category links from their server load.
import type { NavChild, NavLink } from './types';
import type { MessageKey } from './i18n/keys';
import { MODULE_CATALOG, moduleDelegateSections } from './types';
import { MODULE_CATEGORY_I18N, MODULE_CATEGORY_ORDER, categoryHref } from './module-index';
import { identity } from './nav-core';
import { GRANTABLE_SECTIONS, dashboardNavItems as clientNavItems } from './nav-dashboard';

export { navGroups, navItems, resolveSection, type SectionDef } from './nav-core';
export {
  GRANTABLE_SECTIONS, DASHBOARD_SECTIONS, sectionForPath, dashboardNavGroups,
  type GrantSection, type SectionId, type DashboardSectionDef
} from './nav-dashboard';

/** Catalog-backed compatibility entry for server callers. */
export function dashboardNavItems(
  opts: Omit<Parameters<typeof clientNavItems>[0], 'moduleLinks'>
): NavLink[] {
  return clientNavItems({ ...opts, moduleLinks: moduleSectionLinks(opts.t) });
}

/**
 * The sections the /modules page is itself divided into, in the order that page
 * renders them. The rail nests these under Modules; the individual modules are
 * NOT nav entries -- a module is a tile on that page, and only the bespoke
 * href modules own a route, so listing them made the rail disagree with the
 * page it points at. Each href is the same in-page anchor categoryHref() jumps
 * to, and the count is how many modules that section holds.
 */
export function moduleSectionLinks(): (NavChild & { label: MessageKey })[];
export function moduleSectionLinks(t: ((key: MessageKey) => string) | undefined): NavChild[];
export function moduleSectionLinks(t?: (key: MessageKey) => string): NavChild[] {
  const label = t ?? identity;
  return MODULE_CATEGORY_ORDER.map((name) => ({
    href: `/modules${categoryHref(name)}`,
    label: label(MODULE_CATEGORY_I18N[name].label as MessageKey),
    count: MODULE_CATALOG.filter((def) => def.category === name).length
  }));
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
