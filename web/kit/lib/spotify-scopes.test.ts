// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { scopeGap } from './spotify';

const REQUIRED = [
  'user-read-currently-playing',
  'user-read-playback-state',
  'user-modify-playback-state'
];

describe('scopeGap', () => {
  test('a complete grant is not short', () => {
    expect(scopeGap(REQUIRED, REQUIRED)).toEqual([]);
  });

  test('names the scope a pre-playback-control grant is missing', () => {
    expect(scopeGap(REQUIRED, ['user-read-currently-playing', 'user-read-playback-state'])).toEqual(
      ['user-modify-playback-state']
    );
  });

  // The case this surface exists for: custody recorded nothing, so the grant
  // predates scope tracking and has to be treated as stale.
  test('an unknown grant counts as missing everything', () => {
    expect(scopeGap(REQUIRED, [])).toEqual(REQUIRED);
  });

  test('scopes the deployment does not ask for are ignored', () => {
    expect(scopeGap(REQUIRED, [...REQUIRED, 'playlist-read-private'])).toEqual([]);
  });
});
