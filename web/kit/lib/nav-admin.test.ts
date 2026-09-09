// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  ADMIN_GROUP_ORDER,
  ADMIN_SECTIONS,
  adminNavGroups,
  adminNavItems,
  adminSectionForPath,
  adminSectionLabelKey
} from './nav-admin';
import type { StaffRole } from './staff-role';
import en from './i18n/locales/en.json';

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

  test('sub-routes resolve to their owning section', () => {
    expect(adminSectionForPath('/users/12345')).toBe('users');
    expect(adminSectionForPath('/events/stream')).toBe('events');
  });

  test('/analytics is gone and falls back to the overview', () => {
    // The route survives only as a 301 to '/'; it must not claim a nav entry,
    // or the rail would highlight a section the operator can never land on.
    expect(ADMIN_SECTIONS.some((def) => def.href === '/analytics')).toBe(false);
    expect(adminSectionForPath('/analytics')).toBe('overview');
  });

  test('minRole hides Access rows from the roles their routes bounce', () => {
    const hrefs = (role: StaffRole) =>
      adminNavItems({ role, section: 'overview' }).map((item) => item.href);

    // A moderator is bounced by /staff, /audit, /secrets and /counters, so the
    // rail must not offer them: that mismatch is the bug this registry closes.
    expect(hrefs('moderator')).not.toContain('/staff');
    expect(hrefs('moderator')).not.toContain('/counters');
    // /counters demands owner (ROLE_FOR['counters.manage']), not merely admin.
    expect(hrefs('admin')).toContain('/staff');
    expect(hrefs('admin')).not.toContain('/counters');
    expect(hrefs('owner')).toContain('/counters');
    expect(hrefs('owner').length).toBe(ADMIN_SECTIONS.length);
  });

  test('groups follow ADMIN_GROUP_ORDER and drop empty ones', () => {
    // A moderator has no visible Access row at all, so that group must not
    // render as an empty rail heading (or an empty dock popover).
    expect(adminNavGroups({ role: 'moderator', section: 'overview' })).toHaveLength(2);
    expect(adminNavGroups({ role: 'owner', section: 'overview' })).toHaveLength(
      ADMIN_GROUP_ORDER.length
    );
  });

  test('the active section is marked in exactly one group', () => {
    const groups = adminNavGroups({ role: 'owner', section: 'secrets' });
    const active = groups.flatMap((g) => g.items).filter((item) => item.active);
    expect(active.map((item) => item.href)).toEqual(['/secrets']);
  });

  test('every label key is a real English leaf', () => {
    // check-i18n proves en/fr parity, not that a key someone typed here exists;
    // a missing one renders as the raw dot-path in the rail.
    for (const def of ADMIN_SECTIONS) {
      expect(typeof leaf(def.labelKey)).toBe('string');
    }
    // With no `t` injected, navGroups/navItems label with identity, so the
    // label IS the key: look it up the same way.
    for (const role of ROLES) {
      for (const group of adminNavGroups({ role, section: 'overview' })) {
        expect(typeof leaf(group.label ?? '')).toBe('string');
      }
    }
  });

  test('the breadcrumb key of every section is that section label', () => {
    for (const def of ADMIN_SECTIONS) {
      expect(adminSectionLabelKey(def.id)).toBe(def.labelKey);
    }
  });
});
