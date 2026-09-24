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
import { dashboardNavItems as clientNavItems } from './nav-dashboard';

describe('nav registry', () => {
  test('every bespoke page prefix resolves to its owning section', () => {
    expect(sectionForPath('/')).toBe('overview');
    expect(sectionForPath('/commands')).toBe('commands');
    expect(sectionForPath('/commands/new')).toBe('commands');
    for (const p of ['/modules', '/counters', '/quotes', '/govee', '/channelpoints', '/timers', '/loyalty']) {
      expect(sectionForPath(p)).toBe('modules');
    }
    expect(sectionForPath('/discord')).toBe('discord');
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
      '/discord',
      '/billing',
      '/settings'
    ]);
  });

  test('an owner sees all six entries with active following the section', () => {
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
      'en:nav.discord',
      'en:nav.billing',
      'en:nav.settings'
    ]);
    expect(items.map((i) => i.active)).toEqual([false, false, false, false, true, false]);
  });

  test('a delegate sees only granted sections, never owner-only ones', () => {
    const items = dashboardNavItems({ isDelegate: true, sections: ['commands'], section: 'commands' });
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ href: '/commands', active: true });

    const full = dashboardNavItems({
      isDelegate: true,
      sections: [...GRANTABLE_SECTIONS],
      section: 'modules'
    });
    expect(full.map((i) => i.href)).toEqual(['/commands', '/modules', '/discord', '/billing']);
    expect(full.map((i) => i.active)).toEqual([false, true, false, false]);
  });

  test('the rail nests the /modules sections, not the modules themselves', () => {
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

  test('serialized category links preserve translated owner and delegate navigation', () => {
    const wire: ReturnType<typeof moduleSectionLinks> = JSON.parse(JSON.stringify(moduleSectionLinks()));
    for (const sections of [null, [], ['commands'], ['modules'], [...GRANTABLE_SECTIONS]]) {
      for (const locale of ['en', 'fr']) {
        const t = (key: string) => `${locale}:${key}`;
        const opts = { isDelegate: sections !== null, sections: sections ?? [], section: 'modules' as const, t };
        expect(clientNavItems({
          ...opts,
          moduleLinks: wire.map((link) => ({ ...link, label: t(link.label) }))
        })).toEqual(dashboardNavItems(opts));
      }
    }
  });

  test('groups wrap items under the single Manage group', () => {
    const groups = dashboardNavGroups(
      dashboardNavItems({ isDelegate: false, sections: [], section: 'overview' }),
      (key) => `en:${key}`
    );
    expect(groups).toHaveLength(1);
    expect(groups[0].label).toBe('en:nav.manage');
    expect(groups[0].items).toHaveLength(6);
  });
});

describe('delegateAllowedPaths', () => {
  test.each([
    [[], []],
    [['invalid_sec', 'admin'], []],
    [['billing'], ['/billing']]
  ] as [string[], string[]][])('%o opens exactly %o', (sections, paths) => {
    expect(delegateAllowedPaths(sections)).toEqual(paths);
  });

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

  test.each([
    ['/discord', true],
    ['/discord/123456789012345678', true],
    ['/discord/123456789012345678/channels', true],
    ['/discord/123456789012345678/tickets', true],
    ['/commands', false]
  ] as [string, boolean][])('a discord delegate on %s -> %p', (path, want) => {
    expect(pathnameAllowed(path, delegateAllowedPaths(['discord']), ['discord'])).toBe(want);
  });

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
