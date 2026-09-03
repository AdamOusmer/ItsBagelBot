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

// Table-driven on purpose: each of these three functions is a pure
// sections -> verdict map, so the cases ARE the specification. Written out one
// assertion per line they were near-identical blocks that a reader has to diff
// by eye to spot the one differing id.
describe('delegateAllowedPaths', () => {
  // A grant that opens an exact, closed set of paths.
  test.each([
    [[], []],
    [['invalid_sec', 'admin'], []],
    [['billing'], ['/billing']]
  ] as [string[], string[]][])('%o opens exactly %o', (sections, paths) => {
    expect(delegateAllowedPaths(sections)).toEqual(paths);
  });

  // A grant that also pulls in every module page scoped to it.
  test.each([
    ['commands', ['/commands', '/counters/list', '/quotes', '/timers'], ['/billing', '/settings', '/channelpoints']],
    ['channelpoints', ['/channelpoints', '/songqueue'], ['/commands', '/billing', '/counters/list']],
    ['modules', ['/modules', '/counters', '/loyalty', '/quotes', '/timers'], ['/billing', '/settings']]
  ] as [string, string[], string[]][])('the %s grant opens its own pages and no others', (section, open, shut) => {
    const allowed = delegateAllowedPaths([section]);
    for (const path of open) expect(allowed).toContain(path);
    for (const path of shut) expect(allowed).not.toContain(path);
  });
});

describe('pathnameAllowed', () => {
  test('owner-only paths stay denied even to a delegate holding every grant', () => {
    const sections = ['commands', 'modules', 'channelpoints', 'billing'];
    const allowed = delegateAllowedPaths(sections);
    for (const path of ['/', '/settings', '/settings/import', '/substate', '/overview/stream', '/events']) {
      expect(pathnameAllowed(path, allowed, sections)).toBe(false);
    }
  });

  test.each([
    ['/commands', true],
    ['/counters/list', true],
    ['/billing', false]
  ] as [string, boolean][])('a commands delegate on %s -> %p', (path, want) => {
    expect(pathnameAllowed(path, delegateAllowedPaths(['commands']), ['commands'])).toBe(want);
  });

  // The '/modules' prefix covers every catalog module's generic page, so a
  // module with its own narrower delegateSections must be rechecked by id
  // rather than admitted on the bare 'modules' grant.
  test.each([
    [['modules'], '/modules', true],
    [['modules'], '/modules/quotes', true],
    [['modules'], '/modules/channelpoints', false],
    [['modules', 'channelpoints'], '/modules/channelpoints', true]
  ] as [string[], string, boolean][])('%o on %s -> %p', (sections, path, want) => {
    expect(pathnameAllowed(path, delegateAllowedPaths(sections), sections)).toBe(want);
  });
});

describe('moduleSubpathAllowed', () => {
  test.each([
    ['channelpoints', 'modules', false],
    ['channelpoints', 'channelpoints', true],
    ['quotes', 'commands', true],
    ['quotes', 'modules', true],
    ['quotes', 'billing', false],
    ['timers', 'commands', true],
    ['timers', 'modules', true],
    ['timers', 'billing', false]
  ] as [string, string, boolean][])('module %s under a %s grant -> %p', (id, section, want) => {
    expect(moduleSubpathAllowed(id, [section])).toBe(want);
  });
});
