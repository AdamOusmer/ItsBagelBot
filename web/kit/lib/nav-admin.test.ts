// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  ADMIN_GROUP_ORDER,
  ADMIN_GROUP_LABEL,
  ADMIN_SECTIONS,
  adminNavGroups,
  adminNavItems,
  adminSectionForPath,
  adminSectionLabelKey
} from './nav-admin';
import type { StaffRole } from './staff-role';
import { readConsoleTree } from './i18n/tree-fs';

const en = readConsoleTree('en');
const ROLES: StaffRole[] = ['moderator', 'admin', 'owner'];

function leaf(key: string): unknown {
  return key.split('.').reduce<unknown>((node, part) => {
    if (typeof node !== 'object' || node === null) return undefined;
    return (node as Record<string, unknown>)[part];
  }, en);
}

describe('admin nav registry', () => {
  test('every section resolves from its own href', () => {
    for (const def of ADMIN_SECTIONS) {
      expect(adminSectionForPath(def.href)).toBe(def.id);
    }
  });

  test.each<[string, ReturnType<typeof adminSectionForPath>]>([
    ['/users/12345', 'users'],
    ['/events/stream', 'events'],
    ['/deploys/abc123/stream', 'deploys'],
    ['/analytics', 'analytics'],
    ['/settings', 'settings']
  ])('%s resolves to the %s section', (path, section) => {
    expect(adminSectionForPath(path)).toBe(section);
  });

  const links = (role: StaffRole, section: ReturnType<typeof adminSectionForPath> = 'overview') =>
    adminNavGroups({ role, section }).flatMap((group) => group.items);

  const hrefs = (role: StaffRole) =>
    links(role).flatMap((item) => (item.children ?? [item]).map((link) => link.href));

  test.each<[string, StaffRole[]]>([
    ['/staff', ['admin', 'owner']],
    ['/counters', ['owner']],
    ['/trials', ['admin', 'owner']],
    ['/deploys', ['owner']],
    ['/settings', ROLES]
  ])('%s is visible to the permitted staff roles', (path, roles) => {
    expect(ROLES.filter((role) => hrefs(role).includes(path))).toEqual(roles);
  });

  test.each<[StaffRole, string[]]>([
    ['moderator', ['adminNav.overview', ADMIN_GROUP_LABEL.operate, ADMIN_GROUP_LABEL.accounts, 'adminNav.settings']],
    ['owner', ['adminNav.overview', ...ADMIN_GROUP_ORDER.map((id) => ADMIN_GROUP_LABEL[id]), 'adminNav.settings']]
  ])('%s sees overview, the collapsible groups in order, then settings', (role, labels) => {
    expect(links(role).map((item) => item.label)).toEqual(labels);
  });

  test('owners reach every section exactly once', () => {
    expect(hrefs('owner').toSorted()).toEqual(ADMIN_SECTIONS.map((def) => def.href).toSorted());
  });

  test.each<[ReturnType<typeof adminSectionForPath>, string]>([
    ['settings', 'adminNav.settings'],
    ['lanes', ADMIN_GROUP_LABEL.operate],
    ['users', ADMIN_GROUP_LABEL.accounts]
  ])('the %s section marks only %s as current', (section, label) => {
    const active = links('owner', section).filter((item) => item.current);
    expect(active.map((item) => item.label)).toEqual([label]);
  });

  test.each<[number | null, string]>([
    [null, ''],
    [0, '0'],
    [17, '17'],
    [4_294_967_295, '4294967295']
  ])('only Users receives the display value for %p', (liveCount, count) => {
    const opts = { role: 'moderator', section: 'overview', liveCount } as const;
    const items = adminNavItems(opts);
    const children = adminNavGroups(opts)
      .flatMap((group) => group.items)
      .flatMap((item) => item.children ?? []);
    expect(items.filter((item) => item.count !== undefined)).toEqual([
      expect.objectContaining({ href: '/users', count })
    ]);
    expect(children.filter((child) => child.count !== undefined)).toEqual([{ href: '/users', label: 'adminNav.users', count }]);
  });

  test('every label key is a real English leaf, and the breadcrumb key is the section label', () => {
    for (const def of ADMIN_SECTIONS) {
      expect(typeof leaf(def.labelKey)).toBe('string');
      expect(adminSectionLabelKey(def.id)).toBe(def.labelKey);
    }
    for (const key of Object.values(ADMIN_GROUP_LABEL)) {
      expect(typeof leaf(key)).toBe('string');
    }
  });
});
