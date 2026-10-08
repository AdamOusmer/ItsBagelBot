// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { splitWordmark } from '../lib/brand-wordmark';
import { FOOTER_RING, stackColumns } from '../lib/footer-layout';

const column = (title: string, stacked?: boolean) => ({ title, links: [], stacked });

describe('stackColumns', () => {
  test('keeps unstacked columns side by side', () => {
    expect(stackColumns([column('a'), column('b'), column('c')]).map((s) => s.length)).toEqual([1, 1, 1]);
  });

  test('puts a stacked column under the one before it', () => {
    const stacks = stackColumns([column('a'), column('b'), column('c', true), column('d')]);
    expect(stacks.map((stack) => stack.map((c) => c.title))).toEqual([['a'], ['b', 'c'], ['d']]);
  });

  test('chains several stacked columns and ignores a leading stacked flag', () => {
    const stacks = stackColumns([column('a', true), column('b', true), column('c')]);
    expect(stacks.map((stack) => stack.map((c) => c.title))).toEqual([['a', 'b'], ['c']]);
  });

  test('returns no stacks for no columns', () => {
    expect(stackColumns([])).toEqual([]);
  });
});

describe('splitWordmark', () => {
  test('replaces the last o of the brand name', () => {
    expect(splitWordmark('ItsBagelBot')).toEqual(['ItsBagelB', 't']);
  });

  test('matches an uppercase O and keeps the original case around it', () => {
    expect(splitWordmark('ROBOT')).toEqual(['ROB', 'T']);
  });

  test('returns null when the name has no o', () => {
    expect(splitWordmark('Bagel')).toBeNull();
  });

  test('an o at either end leaves an empty side', () => {
    expect(splitWordmark('o')).toEqual(['', '']);
    expect(splitWordmark('Bot')).toEqual(['B', 't']);
  });
});

test('the footer ring references only gradients it defines', () => {
  const used = [...FOOTER_RING.matchAll(/url\(#([^)]+)\)/g)].map((m) => m[1]);
  expect(used.length).toBeGreaterThan(0);
  for (const id of used) expect(FOOTER_RING).toContain(`id="${id}"`);
});
