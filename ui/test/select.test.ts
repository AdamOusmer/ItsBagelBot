// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { filterSelectOptions, nextEnabledOption, normalizeSelectQuery, type SelectOption } from '../lib/select';

const options: SelectOption[] = [
  { value: 'America/Montreal', label: 'Montréal', group: 'America', searchText: 'Québec Canada' },
  { value: 'America/New_York', label: 'New York', description: 'Eastern time', searchText: 'NYC' },
  { value: 'Europe/Paris', label: 'Paris', group: 'Europe', disabled: true },
];

test('normalizes accents, case, underscores and repeated whitespace', () => {
  expect(normalizeSelectQuery('  QUÉBEC__ City\t ')).toBe('quebec city');
});

test('matches all search words across aliases, identifiers, labels, descriptions and groups', () => {
  expect(filterSelectOptions(options, 'america quebec')).toEqual([options[0]]);
  expect(filterSelectOptions(options, 'new_york eastern')).toEqual([options[1]]);
  expect(filterSelectOptions(options, 'NYC')).toEqual([options[1]]);
  expect(filterSelectOptions(options, 'europe paris')).toEqual([options[2]]);
  expect(filterSelectOptions(options, 'canada eastern')).toEqual([]);
  expect(filterSelectOptions(options, '   ')).toEqual(options);
});

test('filtering retains disabled choices and input order without mutating options', () => {
  const before = options.map((option) => ({ ...option }));
  expect(filterSelectOptions(options, 'paris')).toEqual([options[2]]);
  expect(filterSelectOptions(options, 'america')).toEqual(options.slice(0, 2));
  expect(options).toEqual(before);
});

test('keyboard navigation wraps in either direction and skips disabled options', () => {
  const choices = [
    { value: 'a', label: 'A', disabled: true },
    { value: 'b', label: 'B' },
    { value: 'c', label: 'C', disabled: true },
    { value: 'd', label: 'D' },
    { value: 'e', label: 'E', disabled: true },
  ];
  expect(nextEnabledOption(choices, -1, 'first')).toBe(1);
  expect(nextEnabledOption(choices, -1, 'last')).toBe(3);
  expect(nextEnabledOption(choices, 1, 1)).toBe(3);
  expect(nextEnabledOption(choices, 3, 1)).toBe(1);
  expect(nextEnabledOption(choices, 3, -1)).toBe(1);
  expect(nextEnabledOption(choices, 1, -1)).toBe(3);
});

test('empty and fully disabled lists have no keyboard target', () => {
  for (const direction of [1, -1, 'first', 'last'] as const) {
    expect(nextEnabledOption([], -1, direction)).toBe(-1);
    expect(nextEnabledOption([{ value: 'a', label: 'A', disabled: true }], 0, direction)).toBe(-1);
  }
});
