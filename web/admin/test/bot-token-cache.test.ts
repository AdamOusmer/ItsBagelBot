// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, mock, test } from 'bun:test';

const calls: { subject: string; payload: unknown }[] = [];
const replies: Record<string, unknown> = {};
const rpc = mock(async (subject: string, payload: unknown) => {
  calls.push({ subject, payload });
  return replies[subject];
});

mock.module('@bagel/kit/server/nats', () => ({
  rpc,
  publish: mock(async () => {}),
  subscribeDurable: mock(async () => {}),
  RpcError: class RpcError extends Error {}
}));

mock.module('$lib/deploys/types', () => ({ DEPLOY_PREFIX: 'bagel.rpc.admin.deploy', DEPLOY_EVENTS_PREFIX: 'bagel.deploy.events' }));

process.env.NEW_RELIC_ENABLED = 'false';
const { botTokenSet, tokenStatus } = await import('../src/lib/server/services');

describe('bot token re-authorization', () => {
  test('refreshes the cached token status the overview reads', async () => {
    const bot = { actorId: 'bot', userId: 'bot' };
    replies['bagel.rpc.admin.user.token_status'] = { token: { present: false } };
    expect(await tokenStatus(bot)).toEqual({ present: false });

    replies['bagel.rpc.admin.user.bot_token_set'] = { token: { present: true } };
    await botTokenSet(bot, 'access-token', 'refresh-token');

    expect(await tokenStatus(bot)).toEqual({ present: true });
    expect(calls.map((c) => c.subject)).toEqual([
      'bagel.rpc.admin.user.token_status',
      'bagel.rpc.admin.user.bot_token_set'
    ]);
    expect(calls[1].payload).toEqual({
      actor_id: 'bot',
      user_id: 'bot',
      access_token: 'access-token',
      refresh_token: 'refresh-token'
    });
  });
});
