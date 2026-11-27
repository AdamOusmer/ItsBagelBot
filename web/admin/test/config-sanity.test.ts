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

test('admin startup accepts a complete config', () => {
  expect(() => assertConfigSane({ ...sane, NODE_ENV: 'production' })).not.toThrow();
});

test('admin startup rejects a callback on another origin', () => {
  expect(() => assertConfigSane({ ...sane, TWITCH_REDIRECT_URI: 'https://evil.example/auth/callback' })).toThrow(
    'TWITCH_REDIRECT_URI origin https://evil.example does not match https://admin.example'
  );
});
