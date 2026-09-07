// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { canonicalMinecraftUUID } from './minecraft-id';

describe('canonicalMinecraftUUID', () => {
  test('accepts undashed and dashed spellings', () => {
    expect(canonicalMinecraftUUID('deadbeefdeadbeefdeadbeefdeadbeef')).toBe(
      'deadbeefdeadbeefdeadbeefdeadbeef'
    );
    expect(canonicalMinecraftUUID('DEADBEEF-DEAD-BEEF-DEAD-BEEFDEADBEEF')).toBe(
      'deadbeefdeadbeefdeadbeefdeadbeef'
    );
  });

  test('rejects usernames and short hex', () => {
    expect(canonicalMinecraftUUID('Technoblade')).toBeNull();
    expect(canonicalMinecraftUUID('deadbeef')).toBeNull();
    expect(canonicalMinecraftUUID('')).toBeNull();
  });
});
