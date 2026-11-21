// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

export function dashboardNavItems(
  opts: Omit<Parameters<typeof clientNavItems>[0], 'moduleLinks'>
): NavLink[] {
  return clientNavItems({ ...opts, moduleLinks: moduleSectionLinks(opts.t) });
}

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
