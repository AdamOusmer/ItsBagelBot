// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { safeReturnPath } from './return-path';

describe('safeReturnPath', () => {
  test('keeps local paths, queries and fragments', () => {
    for (const path of ['/', '/settings?tab=access#links', '/user/name%20with%20space', '/?next=https%3A%2F%2Fexample.com']) {
      expect(safeReturnPath(path)).toBe(path);
    }
    expect(safeReturnPath('/commands/../settings?tab=access#links')).toBe('/settings?tab=access#links');
  });

  test('rejects external and parser-normalized protocol-relative redirects', () => {
    for (const path of [null, undefined, '', 'settings', 'https://example.com', '//example.com', '/\\example.com', '/\t/example.com', '/\n/example.com', '/\r/example.com', '/\u0000/example.com', '/ /example.com', '/a/..//example.com', '/a/%2e%2e//example.com']) {
      expect(safeReturnPath(path)).toBeNull();
    }
  });
});
