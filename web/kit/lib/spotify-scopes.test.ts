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
  test.each([
    ['a complete grant is not short', REQUIRED, []],
    ['names the scope a pre-playback-control grant is missing', ['user-read-currently-playing', 'user-read-playback-state'], ['user-modify-playback-state']],
    ['an unknown grant counts as missing everything', [], REQUIRED],
    ['scopes the deployment does not ask for are ignored', [...REQUIRED, 'playlist-read-private'], []]
  ] as [string, string[], string[]][])('%s', (_name, granted, missing) => {
    expect(scopeGap(REQUIRED, granted)).toEqual(missing);
  });
});
