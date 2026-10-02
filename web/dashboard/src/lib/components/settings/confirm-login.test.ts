// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { matchesLogin } from './confirm-login';

const cases = [
  { name: 'ignores case and surrounding whitespace', typed: '  StreamerLogin ', login: 'streamerlogin', want: true },
  { name: 'rejects partial input', typed: 'streamer', login: 'streamerlogin', want: false },
  { name: 'rejects empty input', typed: '', login: 'streamerlogin', want: false },
  { name: 'never matches when the login is unknown', typed: '', login: '', want: false }
];

test.each(cases)('matchesLogin ', ({ typed, login, want }) => {
  expect(matchesLogin(typed, login)).toBe(want);
});
