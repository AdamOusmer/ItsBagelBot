// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';

process.env.NEW_RELIC_ENABLED = 'false';
const calls: { subject: string; payload: unknown }[] = [];
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, payload: unknown) => {
    calls.push({ subject, payload });
    return { code: 'single-use-code' };
  }
}));

const { deviceCodeCreate } = await import('./device-auth');

test('approval sends Users the public device-key binding alongside PKCE, never tokens', async () => {
  const dpopJkt = 'a'.repeat(42) + 'A';
  const code = await deviceCodeCreate({
    userId: '42', codeChallenge: 'challenge', dpopJkt,
    redirectUri: 'http://127.0.0.1:51123/callback', deviceName: 'My Laptop', platform: 'macos'
  });
  expect(code).toBe('single-use-code');
  expect(calls).toEqual([{
    subject: 'bagel.rpc.dashboard.device.create_code',
    payload: {
      user_id: '42', code_challenge: 'challenge', dpop_jkt: dpopJkt,
      redirect_uri: 'http://127.0.0.1:51123/callback', device_name: 'My Laptop', platform: 'macos'
    }
  }]);
});
