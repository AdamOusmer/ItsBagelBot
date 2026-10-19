// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { groupsForRoute } from './catalog-groups';

describe('groupsForRoute', () => {
  test('loads nothing extra for routes without lazy namespaces', () => {
    expect(groupsForRoute('/(app)')).toEqual([]);
    expect(groupsForRoute('/(public)/login')).toEqual([]);
    expect(groupsForRoute(null)).toEqual([]);
  });

  test('merges the groups of every matching prefix', () => {
    expect(groupsForRoute('/(app)/settings/import').sort()).toEqual(['editor', 'import']);
    expect(groupsForRoute('/(app)/discord/[guildId]/roles')).toEqual(['discord']);
    expect(groupsForRoute('/(app)/songqueue').sort()).toEqual(['editor', 'import', 'integrations']);
  });
});
