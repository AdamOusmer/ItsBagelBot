// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { scanTokens } from './scan';
import { translateVariables } from './variables';

describe('single-pass variable scanner', () => {
  test('nested composites yield only leaves in source order', () => {
    const text = '$(eval (x) $(USER)) $(query) $(unknown(a(b)))';
    expect([...scanTokens(text)].map((t) => t.raw)).toEqual([
      '$(USER)', '$(query)', '$(unknown(a(b)))'
    ]);
    expect(translateVariables(text).text).toBe('$(eval (x) {user}) {args} $(unknown(a(b)))');
  });

  test('unmatched groups preserve text around balanced leaves', () => {
    const text = ') $(missing $(user) $(bad $(query) tail';
    expect([...scanTokens(text)].map((t) => t.raw)).toEqual(['$(user)', '$(query)']);
    expect(translateVariables(text).text).toBe(') $(missing {user} $(bad {args} tail');
  });

  test('dense unmatched prefixes and deep composites do not rescan suffixes', () => {
    const prefix = '$('.repeat(60000);
    expect([...scanTokens(prefix)]).toEqual([]);
    expect([...scanTokens(`${prefix}user${')'.repeat(60000)}`)].map((t) => t.raw))
      .toEqual(['$(user)']);
  });
});
