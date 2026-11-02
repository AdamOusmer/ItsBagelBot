// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let ignored = 0;
mock.module('newrelic', () => ({
  default: { getTransaction: () => ({ ignore: () => ignored++ }) }
}));
mock.module('#lib/server/live-hub', () => ({ subscribe: () => () => {} }));
const { GET } = await import('./+server');

function open(session: { user_id: string; delegate_of?: string } | null) {
  const abort = new AbortController();
  const res = GET({ locals: { session }, request: new Request('https://x/events', { signal: abort.signal }) } as never);
  abort.abort();
  return res as Response;
}

describe('GET /events', () => {
  beforeEach(() => {
    ignored = 0;
  });

  test('an open stream is dropped from APM', async () => {
    const res = open({ user_id: '42' });
    expect(res.headers.get('content-type')).toBe('text/event-stream');
    await res.body?.cancel();
    expect(ignored).toBe(1);
  });

  test('a rejected request stays in APM', () => {
    expect(open(null).status).toBe(401);
    expect(open({ user_id: '7', delegate_of: '42' }).status).toBe(401);
    expect(ignored).toBe(0);
  });
});
