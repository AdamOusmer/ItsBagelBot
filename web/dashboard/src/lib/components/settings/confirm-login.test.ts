// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { matchesLogin } from './confirm-login';

describe('matchesLogin', () => {
  test('ignores case and surrounding whitespace', () => {
    expect(matchesLogin('  StreamerLogin ', 'streamerlogin')).toBe(true);
  });

  test('rejects partial and empty input', () => {
    expect(matchesLogin('streamer', 'streamerlogin')).toBe(false);
    expect(matchesLogin('', 'streamerlogin')).toBe(false);
  });

  test('never matches when the login is unknown', () => {
    expect(matchesLogin('', '')).toBe(false);
  });
});
