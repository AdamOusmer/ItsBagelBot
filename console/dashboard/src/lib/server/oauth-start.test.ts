// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { skipAuthorizeIfSignedIn } from './oauth-start';

describe('skipAuthorizeIfSignedIn', () => {
  test('starts OAuth for a signed-out visitor', () => {
    expect(
      skipAuthorizeIfSignedIn({ hasSession: false, pendingDelegation: undefined, reauth: null })
    ).toBe(false);
  });

  test('skips OAuth when the marketing CTA hits a live session', () => {
    expect(
      skipAuthorizeIfSignedIn({ hasSession: true, pendingDelegation: undefined, reauth: null })
    ).toBe(true);
  });

  test('still authorizes settings reconnect', () => {
    expect(
      skipAuthorizeIfSignedIn({ hasSession: true, pendingDelegation: undefined, reauth: '1' })
    ).toBe(false);
  });

  test('still authorizes delegate-accept', () => {
    expect(
      skipAuthorizeIfSignedIn({
        hasSession: true,
        pendingDelegation: 'share-token',
        reauth: null
      })
    ).toBe(false);
  });
});
