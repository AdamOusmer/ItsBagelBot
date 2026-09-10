// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The dock's folding rule, tested directly. It is the only DECISION in the
// application shell -- everything else there is layout -- and it used to live
// as four `$derived` expressions inside a Svelte component, where the only way
// to check it was to render an admin board and count buttons.
//
// The cases below are the ones that were reasoned about when the rule was
// written and are the ones a change to it would break: the flat board, the
// hoist, the group the hoist empties, and a count that must not render as `0`.

import { expect, test } from 'bun:test';
import {
  dockGroups,
  groupActive,
  groupCount,
  groupIcon,
  hoistHome,
  isGrouped,
} from '../lib/dock-groups';
import type { UiNavGroup } from '../lib/nav-types';

const board: UiNavGroup[] = [
  {
    label: 'Board',
    items: [
      { href: '/', label: 'Overview', icon: 'overview' },
      { href: '/commands', label: 'Commands', icon: 'commands', count: 4 },
    ],
  },
  {
    label: 'Ops',
    items: [{ href: '/audit', label: 'Audit', icon: 'audit', active: true }],
  },
];

test('one group is a flat dock, several fold', () => {
  expect(isGrouped([board[0]])).toBe(false);
  expect(isGrouped(board)).toBe(true);
});

test('the home route is hoisted out of whichever group holds it', () => {
  expect(hoistHome(board)?.label).toBe('Overview');
  expect(hoistHome(board, '/audit')?.label).toBe('Audit');
  expect(hoistHome([{ items: [{ href: '/x', label: 'X' }] }])).toBeNull();
});

test('a group the hoist empties is dropped, not rendered as an empty popover', () => {
  const single: UiNavGroup[] = [
    { label: 'Home', items: [{ href: '/', label: 'Overview' }] },
    { label: 'Ops', items: [{ href: '/audit', label: 'Audit' }] },
  ];
  expect(dockGroups(single).map((g) => g.label)).toEqual(['Ops']);
});

test('a folded group is current when any page inside it is', () => {
  expect(groupActive(board[1])).toBe(true);
  expect(groupActive(board[0])).toBe(false);
});

test('counts sum, and a zero sum is undefined rather than a "0" badge', () => {
  expect(groupCount(board[0])).toBe(4);
  expect(groupCount(board[1])).toBeUndefined();
});

test('the group glyph falls back to the caller, never to a name from the bot', () => {
  expect(groupIcon(board[0])).toBe('overview');
  expect(groupIcon({ items: [{ href: '/x', label: 'X' }] })).toBeUndefined();
  expect(groupIcon({ items: [{ href: '/x', label: 'X' }] }, 'list')).toBe('list');
});
