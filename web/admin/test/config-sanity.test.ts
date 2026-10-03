// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { expect, test } from 'bun:test';
import { assertConfigSane } from '../src/lib/server/config-sanity';

const sane = {
  ORIGIN: 'https://admin.example',
  TWITCH_REDIRECT_URI: 'https://admin.example/auth/callback',
  DASHBOARD_PUBLIC_ORIGIN: 'https://dashboard.example'
};

test('admin startup rejects DEMO in production', () => {
  expect(() => assertConfigSane({ ...sane, DEMO: '1', NODE_ENV: 'production' })).toThrow('DEMO must not be enabled in production');
});

test.each([
  { name: 'DEMO outside production', env: { ...sane, DEMO: '1', NODE_ENV: 'development' } },
  { name: 'production without DEMO', env: { ...sane, NODE_ENV: 'production' } }
])('admin startup allows $name', ({ env }) => {
  expect(() => assertConfigSane(env)).not.toThrow();
});
