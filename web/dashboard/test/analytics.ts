// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach } from 'bun:test';
import { flushAnalytics, setAnalyticsClientForTests } from '@bagel/kit/server/analytics';

export function captureAnalytics() {
  const counts: [string, number][] = [];
  const pipe = {
    hincrby: (_key: string, field: string, n: number) => (counts.push([field, n]), pipe),
    expireat: () => pipe,
    exec: async () => []
  };
  beforeEach(() => {
    counts.length = 0;
    setAnalyticsClientForTests({ pipeline: () => pipe });
  });
  afterEach(() => setAnalyticsClientForTests(undefined));
  return async () => {
    await flushAnalytics();
    return counts;
  };
}

export function actionEvent(fields: Record<string, string> = {}) {
  return {
    locals: { session: { user_id: '7', login: 'streamer', display_name: 'Streamer' }, locale: 'en' },
    request: new Request('https://dashboard.itsbagelbot.com/', { method: 'POST', body: new URLSearchParams(fields) }),
    getClientAddress: () => '127.0.0.1',
    cookies: { get: () => undefined, delete: () => {} }
  };
}
