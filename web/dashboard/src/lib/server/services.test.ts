// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';

mock.module('newrelic', () => ({
  default: { startSegment: (_n: string, _r: boolean, f: () => unknown) => f(), recordMetric: () => {}, noticeError: () => {} }
}));
mock.module('$app/environment', () => ({ dev: false }));

const { SCOPES, userPrefixes } = await import('./services');

describe('commands_page invalidation routing', () => {
  test('SCOPES.commands_page routes to the per-user commands_page key', () => {
    expect(SCOPES.commands_page?.('42')).toEqual(['commands_page:42']);
  });

  test('userPrefixes includes commands_page so the coarse "*" flush covers it too', () => {
    expect(userPrefixes('42')).toContain('commands_page:42');
  });
});
