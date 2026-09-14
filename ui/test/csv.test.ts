// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { csvCell, csvDocument } from '../lib/csv';

test('CSV protects commas, quotes and either newline spelling', () => {
  expect(csvDocument('name,note', [['A,B', 'said "hello"'], ['C', 'line\rbreak'], ['D', 'line\nbreak']]))
    .toBe('name,note\n"A,B","said ""hello"""\nC,"line\rbreak"\nD,"line\nbreak"');
  expect(csvCell('plain')).toBe('plain');
  expect(csvCell('')).toBe('');
  expect(csvDocument('name', [])).toBe('name');
});
