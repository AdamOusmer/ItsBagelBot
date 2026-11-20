// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { LOGIN_RE, USERNAME_RE } from './twitch-names';

type Case = { value: string; login: boolean; username: boolean };
const { logins } = JSON.parse(
  readFileSync(new URL('../../../internal/domain/validate/testdata/twitch_names.json', import.meta.url), 'utf8')
) as { logins: Case[] };

test('login patterns match the Go Twitch name table', () => {
  const got = logins.map(({ value }) => [value, LOGIN_RE.test(value), USERNAME_RE.test(value)]);
  expect(got).toEqual(logins.map(({ value, login, username }) => [value, login, username]));
});
