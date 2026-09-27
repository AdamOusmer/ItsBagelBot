// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { expect, mock, test } from 'bun:test';

mock.module('$env/dynamic/private', () => ({ env: {
  DASHBOARD_TWITCH_CLIENT_ID: 'bot-client',
  DASHBOARD_TWITCH_CLIENT_SECRET: 'test-secret',
  // Stale deployment overrides must not strip the bot's required permissions.
  BOT_OAUTH_SCOPES: 'openid moderator:read:followers'
} }));
const { botScopes, botTwitch } = await import('../src/lib/server/oauth');

test('bot consent requests watchtime, moderator verification, and cloud bot permissions', () => {
  const url = botTwitch('https://admin.example').createAuthorizationURL('state', botScopes());
  const requested = new Set(url.searchParams.get('scope')?.split(' '));
  for (const required of ['moderator:read:chatters', 'user:read:moderated_channels', 'user:bot']) {
    expect(requested.has(required)).toBe(true);
  }
  expect(url.searchParams.get('client_id')).toBe('bot-client');
});
