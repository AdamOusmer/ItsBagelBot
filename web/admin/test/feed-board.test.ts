// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, mock, test } from 'bun:test';

const calls: { subject: string; payload: unknown }[] = [];
let reply: unknown = {};
const rpc = mock(async (subject: string, payload: unknown) => {
  calls.push({ subject, payload });
  return reply;
});

mock.module('@bagel/kit/server/nats', () => ({
  rpc,
  publish: mock(async () => {}),
  subscribeDurable: mock(async () => {}),
  RpcError: class RpcError extends Error {}
}));

process.env.NEW_RELIC_ENABLED = 'false';
const { feedBoard, FEED_BOARD_SIZE } = await import('../src/lib/server/services');

describe('admin feed board read', () => {
  test('asks modules for the top channels and keeps counts exact past the float range', async () => {
    calls.length = 0;
    reply = {
      total: '9007199254740993',
      ranked: 3,
      entries: [
        { broadcaster_id: 51496027, name: ' cozycrumbs ', count: '9007199254740992' },
        { broadcaster_id: 28807154, count: 4 }
      ]
    };

    expect(await feedBoard()).toEqual({
      total: '9007199254740993',
      ranked: '3',
      entries: [
        { id: '51496027', name: 'cozycrumbs', count: '9007199254740992' },
        { id: '28807154', name: '', count: '4' }
      ]
    });
    expect(calls).toEqual([{ subject: 'bagel.rpc.modules.personality.feed.board', payload: { limit: FEED_BOARD_SIZE } }]);
  });

  test('an untouched bagel reads as zero with no channels', async () => {
    reply = { total: '0', ranked: 0 };
    expect(await feedBoard()).toEqual({ total: '0', ranked: '0', entries: [] });
  });

  test('a refusal or a count that is not a nonnegative integer fails the read', async () => {
    reply = { error: 'unavailable' };
    await expect(feedBoard()).rejects.toThrow('unavailable');

    reply = { total: '-1', ranked: 0 };
    await expect(feedBoard()).rejects.toThrow('invalid feed count');

    reply = { total: '5', ranked: 1, entries: [{ broadcaster_id: 1, name: 'x', count: 'many' }] };
    await expect(feedBoard()).rejects.toThrow('invalid feed count');
  });
});
