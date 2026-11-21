// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { hasOwnerAccess, isSelfSignIn } from './owner';
import type { Session } from './session';

const base: Session = { user_id: '42', login: 'streamer', display_name: 'Streamer', role: 'streamer', sid: 's', iat: 1, expires_at: 2 };

const cases: { name: string; session: Session | null; ownerAccess: boolean; selfSignIn: boolean }[] = [
  { name: 'account holder', session: base, ownerAccess: true, selfSignIn: true },
  { name: 'staff view-as', session: { ...base, impersonator_id: '7', impersonator_login: 'staff' }, ownerAccess: true, selfSignIn: false },
  { name: 'delegate', session: { ...base, user_id: '95', delegate_of: '42', sections: ['commands'] }, ownerAccess: false, selfSignIn: false },
  { name: 'signed out', session: null, ownerAccess: false, selfSignIn: false }
];

test.each(cases)('owner access policy: $name', ({ session, ownerAccess, selfSignIn }) => {
  expect({ ownerAccess: hasOwnerAccess(session), selfSignIn: isSelfSignIn(session) }).toEqual({ ownerAccess, selfSignIn });
});
