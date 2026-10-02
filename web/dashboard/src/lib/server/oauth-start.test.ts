// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { skipAuthorizeIfSignedIn } from './oauth-start';

const cases = [
  { name: 'starts OAuth for a signed-out visitor', visit: { hasSession: false, pendingDelegation: undefined, reauth: null }, want: false },
  { name: 'skips OAuth when the marketing CTA hits a live session', visit: { hasSession: true, pendingDelegation: undefined, reauth: null }, want: true },
  { name: 'still authorizes settings reconnect', visit: { hasSession: true, pendingDelegation: undefined, reauth: '1' }, want: false },
  { name: 'still authorizes delegate-accept', visit: { hasSession: true, pendingDelegation: 'share-token', reauth: null }, want: false }
];

test.each(cases)('skipAuthorizeIfSignedIn ', ({ visit, want }) => {
  expect(skipAuthorizeIfSignedIn(visit)).toBe(want);
});
