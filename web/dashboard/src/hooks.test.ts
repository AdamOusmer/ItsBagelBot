// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { reroute } from './hooks';

const route = (href: string) => reroute({ url: new URL(href), fetch: fetch } as never);

test.each([
  ['https://stats.itsbagelbot.com/', '/stats'],
  ['https://stats.itsbagelbot.com/streamer', undefined],
  ['https://queue.itsbagelbot.com/streamer', '/queue/streamer'],
  ['https://queue.itsbagelbot.com/streamer/', '/queue/streamer'],
  ['https://queue.itsbagelbot.com/@Streamer', '/queue/@Streamer'],
  ['https://queue.itsbagelbot.com/streamer/data', '/queue/streamer/data'],
  ['https://queue.itsbagelbot.com/queue/streamer', undefined],
  ['https://queue.itsbagelbot.com/', undefined],
  ['https://queue.itsbagelbot.com/robots.txt', undefined],
  ['https://queue.itsbagelbot.com/streamer/other', undefined],
  ['https://leaderboard.itsbagelbot.com/streamer', undefined],
  ['https://commands.itsbagelbot.com/streamer/data', undefined]
])('%s routes to %s', (href, want) => {
  expect(route(href)).toBe(want);
});

test.each(['status', 'healthz', 'readyz', 'login', 'stats', 'auth', 'delegate', 'spotify', 'lang', 'cursor', 'events'])(
  'the %s route keeps its own handler on the queue host',
  (name) => {
    expect(route(`https://queue.itsbagelbot.com/${name}`)).toBeUndefined();
  }
);
