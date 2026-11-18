// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { registerServerConfig } from '@bagel/kit/server/config';
import { privateEnv, stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
privateEnv.SPOTIFY_REDIRECT_URI = 'https://dashboard.test/spotify/callback';
registerServerConfig({ cacheInvalidationPrefix: 'inv' });

let tokenStored = false;
const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...realNats,
  publish: async () => {},
  rpc: async (subject: string) => {
    if (subject.endsWith('.spotify.exchange')) return { refresh_token: 'refresh', scopes: [] };
    if (subject.endsWith('.set')) {
      tokenStored = true;
      return {};
    }
    return { present: tokenStored };
  }
}));
const upserts: { name: string; enabled: boolean }[] = [];
mock.module('#lib/server/commands-store', () => ({
  listModules: async () => [],
  upsertModule: async (_uid: string, name: string, enabled: boolean) => {
    upserts.push({ name, enabled });
    return { modules: [], synced: true };
  }
}));

const { GET } = await import('./+server');

async function callback(): Promise<string> {
  const event = {
    url: new URL('https://dashboard.test/spotify/callback?code=c&state=s'),
    cookies: { get: () => 's', delete: () => {} },
    locals: { session: { user_id: '7' }, locale: 'en' } as unknown as App.Locals
  };
  try {
    await (GET as unknown as (e: typeof event) => Promise<Response>)(event);
  } catch (thrown) {
    return (thrown as { location: string }).location;
  }
  throw new Error('callback did not redirect');
}

test.each([
  { name: 'a first connect turns song requests on', stored: false, want: [{ name: 'songqueue', enabled: true }] },
  { name: 'a reconnect keeps the switch as it was', stored: true, want: [] }
])('$name', async ({ stored, want }) => {
  tokenStored = stored;
  upserts.length = 0;
  expect(await callback()).toBe('/songqueue?connected=1');
  expect(upserts).toEqual([...want]);
});
