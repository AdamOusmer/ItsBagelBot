// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import type { HookEvent } from './hooks';

mock.module('virtual:bagel-i18n', () => ({ urls: {}, loaders: {}, sliced: false, localeNames: {}, locales: ['en'] }));

const { tagTransaction } = await import('./hooks');

function fakeAgent() {
  const calls = { ignored: 0, names: [] as string[], attributes: [] as object[] };
  const agent = {
    getTransaction: () => ({ ignore: () => void calls.ignored++ }),
    setTransactionName: (name: string) => void calls.names.push(name),
    addCustomAttributes: (atts: object) => void calls.attributes.push(atts)
  };
  return { agent, calls };
}

function eventWith(accept: string | null, routeId: string | null): HookEvent {
  const headers = accept ? { accept } : undefined;
  return {
    url: new URL('https://console.example/x'),
    request: new Request('https://console.example/x', { headers }),
    route: { id: routeId }
  } as HookEvent;
}

describe('tagTransaction', () => {
  test('ignores event-stream requests and leaves them untagged', () => {
    const { agent, calls } = fakeAgent();
    tagTransaction(agent, eventWith('text/event-stream', '/events'), null);
    expect(calls).toEqual({ ignored: 1, names: [], attributes: [] });
  });

  test('names and tags every other request', () => {
    const { agent, calls } = fakeAgent();
    tagTransaction(agent, eventWith('text/html', '/overview'), { user_id: '1' });
    expect(calls.ignored).toBe(0);
    expect(calls.names).toEqual(['GET /overview']);
    expect(calls.attributes).toEqual([
      { 'route.id': '/overview', 'http.method': 'GET', 'enduser.authenticated': true }
    ]);
  });
});
