// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { MODULE_CATALOG } from './catalog';
import { DASHBOARD_SECTIONS, GRANTABLE_SECTIONS, dashboardNavGroups, dashboardNavItems, sectionForPath } from './nav';

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

  test('groups wrap items under the single Manage group', () => {
    const groups = dashboardNavGroups(
      dashboardNavItems({ isDelegate: false, sections: [], section: '/' }),
      (key) => `en:${key}`
    );
    expect(groups).toHaveLength(1);
    expect(groups[0].label).toBe('en:nav.manage');
    expect(groups[0].items).toHaveLength(5);
  });

  // Subsections: modules derives its children from the catalog, so a new
  // module's category joins the sidebar without touching this file — but the
  // grouping rules (order, hidden exclusion, anchor scheme) stay pinned.
  test('module subsections group the catalog by category in first-appearance order', () => {
    const expected: string[] = [];
    for (const m of MODULE_CATALOG) {
      if (!m.hidden && !expected.includes(m.category)) expected.push(m.category);
    }
    const items = dashboardNavItems({ isDelegate: false, sections: [], section: 'overview' });
    const children = items.find((i) => i.href === '/modules')!.children!;
    expect(children.map((c) => c.label)).toEqual(expected);
    // automod is the catalog's one hidden module; with it goes the whole
    // Moderation category (the hub renders no heading for it either).
    expect(children.map((c) => c.label)).not.toContain('Moderation');
    // Anchors must match the hub page's scrollspy id scheme exactly
    // ('cat-' + slugified category) or the links strand mid-page.
    expect(children.map((c) => c.href)).toEqual([
      '/modules#cat-chat-tools',
      '/modules#cat-community',
      '/modules#cat-games'
    ]);
  });

  test('subsection children ride their parent gate, none of their own', () => {
    // A billing-only delegate gets one entry with no children — commands and
    // settings subsections vanish with their parents.
    const items = dashboardNavItems({ isDelegate: true, sections: ['billing'], section: 'billing' });
    expect(items.map((i) => i.href)).toEqual(['/billing']);
    expect(items[0].children).toBeUndefined();

    // Fully granted delegates keep the children of the sections they hold;
    // owner-only Settings still contributes none.
    const full = dashboardNavItems({
      isDelegate: true,
      sections: [...GRANTABLE_SECTIONS],
      section: 'modules'
    });
    expect(full.find((i) => i.href === '/commands')!.children!.map((c) => c.href)).toEqual([
      '/commands',
      '/commands/fetches'
    ]);
    expect(full.find((i) => i.href === '/modules')!.children).toHaveLength(3);
    expect(full.some((i) => i.href === '/settings')).toBe(false);
  });

  test('child active flags: hash on the module hub, exact path elsewhere', () => {
    const hub = dashboardNavItems({
      isDelegate: false,
      sections: [],
      section: 'overview',
      path: '/modules',
      hash: '#cat-community'
    });
    const hubChildren = hub.find((i) => i.href === '/modules')!.children!;
    expect(hubChildren.map((c) => !!c.active)).toEqual([false, true, false]);
    // The parent stays section-driven: overview section means inactive here.
    expect(hub.find((i) => i.href === '/modules')!.active).toBe(false);

    // Static children light only on an exact path match; no hash means every
    // module child reads inactive even on the hub.
    const access = dashboardNavItems({
      isDelegate: false,
      sections: [],
      section: 'settings',
      path: '/access'
    });
    expect(access.find((i) => i.href === '/settings')!.children!.map((c) => !!c.active)).toEqual([
      false,
      true,
      false
    ]);
    const bare = dashboardNavItems({ isDelegate: false, sections: [], section: 'modules', path: '/modules' });
    expect(bare.find((i) => i.href === '/modules')!.children!.some((c) => c.active)).toBe(false);
  });
});
