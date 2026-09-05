// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { randomBytes } from 'node:crypto';
import { openOAuthState, sealOAuthState } from './oauth-state';

const key = randomBytes(32);
const other = randomBytes(32);
const STATE = 'zH8sB6xQ2m1kJ0pR';
const UID = '44322889';

describe('oauth-state', () => {
  test('a sealed state round trips', () => {
    const cookie = sealOAuthState(key, 'discord-pick', UID, STATE);
    expect(cookie.startsWith(`${STATE}.`)).toBe(true);
    expect(openOAuthState(key, 'discord-pick', UID, cookie)).toBe(STATE);
  });

  test('a cookie minted for another account does not validate', () => {
    const cookie = sealOAuthState(key, 'discord-pick', UID, STATE);
    expect(openOAuthState(key, 'discord-pick', '99999999', cookie)).toBeNull();
  });

  test('the two Discord legs do not validate for each other', () => {
    const cookie = sealOAuthState(key, 'discord-pick', UID, STATE);
    expect(openOAuthState(key, 'discord-install', UID, cookie)).toBeNull();
  });

  test('another key does not validate', () => {
    const cookie = sealOAuthState(key, 'discord-pick', UID, STATE);
    expect(openOAuthState(other, 'discord-pick', UID, cookie)).toBeNull();
  });

  test('a swapped state does not validate', () => {
    const cookie = sealOAuthState(key, 'discord-pick', UID, STATE);
    const tampered = `attacker-state.${cookie.split('.')[1]}`;
    expect(openOAuthState(key, 'discord-pick', UID, tampered)).toBeNull();
  });

  test('malformed cookies are refused, not thrown on', () => {
    for (const bad of ['', '.', 'nomac', `${STATE}.`, `.${STATE}`, `${STATE}.!!!!`]) {
      expect(openOAuthState(key, 'discord-pick', UID, bad)).toBeNull();
    }
  });

  test('label and uid cannot be re-split into a different pair', () => {
    // "ab" + "cd" and "a" + "bcd" concatenate identically; the length prefix is
    // what keeps them apart.
    const a = sealOAuthState(key, 'ab', 'cd', STATE);
    expect(openOAuthState(key, 'a', 'bcd', a)).toBeNull();
  });
});
