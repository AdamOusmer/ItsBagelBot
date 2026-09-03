// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  DASHBOARD_SECTIONS,
  GRANTABLE_SECTIONS,
  dashboardNavGroups,
  dashboardNavItems,
  moduleSectionLinks,
  sectionForPath,
  delegateAllowedPaths,
  pathnameAllowed,
  moduleSubpathAllowed
} from './nav';
import { MODULE_CATALOG } from './types';
import { MODULE_CATEGORY_ORDER } from './module-index';

describe('nav registry', () => {
  test('every bespoke page prefix resolves to its owning section', () => {
    // Counters/quotes/govee/timers/loyalty have no nav entry of their own; they
    // must breadcrumb as Modules or the old four-place drift is back.
    expect(sectionForPath('/')).toBe('overview');
    expect(sectionForPath('/commands')).toBe('commands');
    expect(sectionForPath('/commands/new')).toBe('commands');
    for (const p of ['/modules', '/counters', '/quotes', '/govee', '/channelpoints', '/timers', '/loyalty']) {
      expect(sectionForPath(p)).toBe('modules');
    }
    expect(sectionForPath('/billing')).toBe('billing');
    expect(sectionForPath('/settings')).toBe('settings');
    expect(sectionForPath('/access')).toBe('settings');
    expect(sectionForPath('/nowhere')).toBe('overview');
  });

  test('dock order and hrefs match the registry', () => {
    expect(DASHBOARD_SECTIONS.map((def) => def.href)).toEqual([
      '/',
      '/commands',
      '/modules',
      '/billing',
      '/settings'
    ]);
  });

  test('an owner sees all five entries with active following the section', () => {
    const items = dashboardNavItems({
      isDelegate: false,
      sections: [],
      section: 'billing',
      t: (key) => `en:${key}`
    });
    expect(items.map((i) => i.label)).toEqual([
      'en:nav.overview',
      'en:nav.commands',
      'en:nav.modules',
      'en:nav.billing',
      'en:nav.settings'
    ]);
    expect(items.map((i) => i.active)).toEqual([false, false, false, true, false]);
  });

  // Delegate visibility: ownerOnly entries (Overview, Settings) drop; grants
  // gate the rest. A commands-only delegate gets exactly one entry — this is
  // the shape guard.ts bounce logic assumes.
  test('a delegate sees only granted sections, never owner-only ones', () => {
    const items = dashboardNavItems({ isDelegate: true, sections: ['commands'], section: 'commands' });
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ href: '/commands', active: true });

    const full = dashboardNavItems({
      isDelegate: true,
      sections: [...GRANTABLE_SECTIONS],
      section: 'modules'
    });
    expect(full.map((i) => i.href)).toEqual(['/commands', '/modules', '/billing']);
    expect(full.map((i) => i.active)).toEqual([false, true, false]);
  });

  test('the rail nests the /modules sections, not the modules themselves', () => {
    // A module is a tile on /modules, not a page of its own (only 8 of the 21
    // have a route), so the rail must mirror that page's sections instead.
    const links = moduleSectionLinks((key) => `en:${key}`);
    expect(links.map((l) => l.href)).toEqual(
      MODULE_CATEGORY_ORDER.map((name) => `/modules#cat-${name.toLowerCase()}`)
    );
    expect(links.map((l) => l.count).reduce((a, b) => Number(a) + Number(b), 0)).toBe(
      MODULE_CATALOG.length
    );
    expect(links[0].label).toBe('en:modules.catModeration');
  });

  test('only the Modules row carries children', () => {
    const items = dashboardNavItems({ isDelegate: false, sections: [], section: 'overview' });
    const withKids = items.filter((i) => i.children?.length);
    expect(withKids.map((i) => i.href)).toEqual(['/modules']);
    expect(withKids[0].children).toHaveLength(MODULE_CATEGORY_ORDER.length);
  });

  test('groups wrap items under the single Manage group', () => {
    const groups = dashboardNavGroups(
      dashboardNavItems({ isDelegate: false, sections: [], section: 'overview' }),
      (key) => `en:${key}`
    );
    expect(groups).toHaveLength(1);
    expect(groups[0].label).toBe('en:nav.manage');
    expect(groups[0].items).toHaveLength(5);
  });
});

describe('delegateAllowedPaths', () => {
  test('empty sections allow no paths', () => {
    expect(delegateAllowedPaths([])).toEqual([]);
  });

  test('unknown sections are filtered out', () => {
    expect(delegateAllowedPaths(['invalid_sec', 'admin'])).toEqual([]);
  });

  test('commands grant allows commands, counters list, and commands-scoped modules', () => {
    const allowed = delegateAllowedPaths(['commands']);
    expect(allowed).toContain('/commands');
    expect(allowed).toContain('/counters/list');
    expect(allowed).toContain('/quotes');
    expect(allowed).toContain('/timers');
    expect(allowed).not.toContain('/billing');
    expect(allowed).not.toContain('/settings');
    expect(allowed).not.toContain('/channelpoints');
  });

  test('billing grant allows only billing', () => {
    const allowed = delegateAllowedPaths(['billing']);
    expect(allowed).toEqual(['/billing']);
    expect(allowed).not.toContain('/commands');
    expect(allowed).not.toContain('/counters/list');
    expect(allowed).not.toContain('/modules');
    expect(allowed).not.toContain('/settings');
  });

  test('channelpoints grant allows channel points and song queue', () => {
    const allowed = delegateAllowedPaths(['channelpoints']);
    expect(allowed).toContain('/channelpoints');
    expect(allowed).toContain('/songqueue');
    expect(allowed).not.toContain('/commands');
    expect(allowed).not.toContain('/billing');
    expect(allowed).not.toContain('/counters/list');
  });

  test('modules grant allows modules and module pages', () => {
    const allowed = delegateAllowedPaths(['modules']);
    expect(allowed).toContain('/modules');
    expect(allowed).toContain('/counters');
    expect(allowed).toContain('/loyalty');
    expect(allowed).toContain('/quotes');
    expect(allowed).toContain('/timers');
    expect(allowed).not.toContain('/billing');
    expect(allowed).not.toContain('/settings');
  });
});

describe('pathnameAllowed', () => {
  test('owner-only paths are strictly denied to delegates', () => {
    const allSections = ['commands', 'modules', 'channelpoints', 'billing'] as const;
    const allowed = delegateAllowedPaths(allSections);

    expect(pathnameAllowed('/', allowed, allSections)).toBe(false);
    expect(pathnameAllowed('/settings', allowed, allSections)).toBe(false);
    expect(pathnameAllowed('/settings/import', allowed, allSections)).toBe(false);
    expect(pathnameAllowed('/substate', allowed, allSections)).toBe(false);
    expect(pathnameAllowed('/overview/stream', allowed, allSections)).toBe(false);
    expect(pathnameAllowed('/events', allowed, allSections)).toBe(false);
  });

  test('exact hits and legitimate subpaths pass', () => {
    const allowed = delegateAllowedPaths(['commands']);
    expect(pathnameAllowed('/commands', allowed, ['commands'])).toBe(true);
    expect(pathnameAllowed('/counters/list', allowed, ['commands'])).toBe(true);
    expect(pathnameAllowed('/billing', allowed, ['commands'])).toBe(false);
  });

  test('narrower module scope blocks access under /modules prefix without the grant', () => {
    // Channelpoints requires channelpoints section, even under /modules
    const modulesOnlyAllowed = delegateAllowedPaths(['modules']);
    expect(pathnameAllowed('/modules', modulesOnlyAllowed, ['modules'])).toBe(true);
    expect(pathnameAllowed('/modules/quotes', modulesOnlyAllowed, ['modules'])).toBe(true);
    expect(pathnameAllowed('/modules/channelpoints', modulesOnlyAllowed, ['modules'])).toBe(false);

    // With channelpoints section, access is permitted
    const cpAllowed = delegateAllowedPaths(['modules', 'channelpoints']);
    expect(pathnameAllowed('/modules/channelpoints', cpAllowed, ['modules', 'channelpoints'])).toBe(true);
  });
});

describe('moduleSubpathAllowed', () => {
  test('blocks channelpoints when delegate lacks channelpoints section', () => {
    expect(moduleSubpathAllowed('channelpoints', ['modules'])).toBe(false);
    expect(moduleSubpathAllowed('channelpoints', ['channelpoints'])).toBe(true);
  });

  test('permits quotes for both modules and commands delegates', () => {
    expect(moduleSubpathAllowed('quotes', ['commands'])).toBe(true);
    expect(moduleSubpathAllowed('quotes', ['modules'])).toBe(true);
    expect(moduleSubpathAllowed('quotes', ['billing'])).toBe(false);
  });

  test('permits timers for both modules and commands delegates', () => {
    expect(moduleSubpathAllowed('timers', ['commands'])).toBe(true);
    expect(moduleSubpathAllowed('timers', ['modules'])).toBe(true);
    expect(moduleSubpathAllowed('timers', ['billing'])).toBe(false);
  });
});
