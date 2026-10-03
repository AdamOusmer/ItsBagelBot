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
  const cookie = sealOAuthState(key, 'discord-pick', UID, STATE);

  test('a sealed state round trips', () => {
    expect(cookie.startsWith(`${STATE}.`)).toBe(true);
    expect(openOAuthState(key, 'discord-pick', UID, cookie)).toBe(STATE);
  });

  test.each([
    ['a cookie minted for another account does not validate', () => openOAuthState(key, 'discord-pick', '99999999', cookie)],
    ['the two Discord legs do not validate for each other', () => openOAuthState(key, 'discord-install', UID, cookie)],
    ['another key does not validate', () => openOAuthState(other, 'discord-pick', UID, cookie)],
    ['a swapped state does not validate', () => openOAuthState(key, 'discord-pick', UID, `attacker-state.${cookie.split('.')[1]}`)],
    ['label and uid cannot be re-split into a different pair', () => openOAuthState(key, 'a', 'bcd', sealOAuthState(key, 'ab', 'cd', STATE))],
    ...['', '.', 'nomac', `${STATE}.`, `.${STATE}`, `${STATE}.!!!!`].map(
      (bad) => [`a malformed cookie ${JSON.stringify(bad)} is refused, not thrown on`, () => openOAuthState(key, 'discord-pick', UID, bad)] as [string, () => string | null]
    )
  ] as [string, () => string | null][])('%s', (_name, open) => {
    expect(open()).toBeNull();
  });
});
