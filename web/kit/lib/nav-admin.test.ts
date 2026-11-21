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

  const hrefs = (role: StaffRole) =>
    adminNavGroups({ role, section: 'overview' })
      .flatMap((group) => group.items)
      .map((item) => item.href);

  test.each<[string, StaffRole[]]>([
    ['/staff', ['admin', 'owner']],
    ['/counters', ['owner']],
    ['/trials', ['admin', 'owner']],
    ['/deploys', ['owner']],
    ['/settings', ROLES]
  ])('%s is visible to the permitted staff roles', (path, roles) => {
    expect(ROLES.filter((role) => hrefs(role).includes(path))).toEqual(roles);
  });

  test('groups follow ADMIN_GROUP_ORDER and drop empty ones', () => {
    const labels = (role: StaffRole) =>
      adminNavGroups({ role, section: 'overview' }).map((group) => group.label);
    expect(labels('moderator')).toEqual([ADMIN_GROUP_LABEL.operate, ADMIN_GROUP_LABEL.accounts]);
    expect(labels('owner')).toEqual(ADMIN_GROUP_ORDER.map((id) => ADMIN_GROUP_LABEL[id]));
    expect(hrefs('owner')).toHaveLength(ADMIN_SECTIONS.length);
  });

  test('the active section is marked in exactly one group', () => {
    const groups = adminNavGroups({ role: 'moderator', section: 'settings' });
    const active = groups.flatMap((g) => g.items).filter((item) => item.current);
    expect(active.map((item) => item.href)).toEqual(['/settings']);
  });

  test.each<[number | null, string]>([
    [null, ''],
    [0, '0'],
    [17, '17'],
    [4_294_967_295, '4294967295']
  ])('only Users receives the display value for %p', (liveCount, count) => {
    const opts = { role: 'moderator', section: 'overview', liveCount } as const;
    const items = adminNavItems(opts);
    const grouped = adminNavGroups(opts).flatMap((group) => group.items);
    expect(items.filter((item) => item.count !== undefined)).toEqual([
      expect.objectContaining({ href: '/users', count })
    ]);
    expect(grouped).toEqual(items);
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
