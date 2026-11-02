// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();

let ignored = 0;
mock.module('newrelic', () => ({
  default: { getTransaction: () => ({ ignore: () => ignored++ }) }
}));
mock.module('#lib/server/overview-lanes', () => ({ overviewLanes: () => ({}) }));
mock.module('#lib/server/sse-broadcast', () => ({
  createBroadcast: () => ({}),
  broadcastResponse: () => new Response('', { headers: { 'content-type': 'text/event-stream' } })
}));
const { GET } = await import('./+server');

const open = (session: { user_id: string; delegate_of?: string } | null) =>
  GET({ locals: { session }, request: new Request('https://x/overview/stream') } as never) as Response;

describe('GET /overview/stream', () => {
  beforeEach(() => {
    ignored = 0;
  });

  test('an open stream is dropped from APM', () => {
    expect(open({ user_id: '42' }).headers.get('content-type')).toBe('text/event-stream');
    expect(ignored).toBe(1);
  });

  test('a rejected request stays in APM', () => {
    expect(open(null).status).toBe(401);
    expect(open({ user_id: '7', delegate_of: '42' }).status).toBe(401);
    expect(ignored).toBe(0);
  });
});
