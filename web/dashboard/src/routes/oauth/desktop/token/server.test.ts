// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { POST } from './+server';

describe('retired dashboard desktop token endpoint', () => {
  test('exchange and refresh are retired without invalidating the device grant', async () => {
    for (const grantType of ['authorization_code', 'refresh_token']) {
      const request = new Request('https://dashboard.itsbagelbot.com/oauth/desktop/token', {
        method: 'POST',
        body: new URLSearchParams({ grant_type: grantType, refresh_token: 'disposable-secret' })
      });
      const response = await POST({ request } as never);
      expect(response.status).toBe(410);
      expect(await response.json()).toEqual({ error: 'server_error' });
      expect(response.headers.get('cache-control')).toBe('no-store');
      expect(response.headers.get('pragma')).toBe('no-cache');
      expect(response.headers.has('location')).toBe(false);
      expect(response.headers.has('set-cookie')).toBe(false);
      expect(response.headers.has('access-control-allow-origin')).toBe(false);
    }
  });

  test('retirement does not read credentials, request bodies, or dashboard cookies', async () => {
    const event = new Proxy({}, { get() { throw new Error('retired route must not inspect credentials'); } });
    const response = await POST(event as never);
    expect(response.status).toBe(410);
    expect(await response.text()).toBe('{"error":"server_error"}');
  });
});
