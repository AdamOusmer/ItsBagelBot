// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { isRedirect, type Cookies } from '@sveltejs/kit';
import { registerServerConfig } from '@bagel/kit/server/config';
import { privateEnv, stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
process.env.SESSION_KEY = Buffer.alloc(32, 7).toString('base64');
privateEnv.RIOT_RSO_CLIENT_ID = 'riot-client';
const redirectUri = 'https://dashboard.test/riot/callback';
privateEnv.RIOT_RSO_REDIRECT_URI = redirectUri;
registerServerConfig({ cacheInvalidationPrefix: 'inv' });

const account = { puuid: 'riot-player', game_name: 'Frosty', tag_line: 'EUW1', shard: 'eu' };
const commands = { region: 'eu', platform: 'pc' };
const calls: { subject: string; payload: unknown }[] = [];
const upserts: { uid: string; enabled: boolean; config: unknown }[] = [];
let linked = false;
let refused = '';
let replyOverride: { subject: string; value: unknown } | undefined;
const realNats = await import('@bagel/kit/server/nats');

const reply = {
  'rpc.gossip.valorant.link': account,
  'rpc.modules.riot.link.status': () => ({ linked }),
  'rpc.modules.riot.link.set': {},
  'rpc.modules.riot.link.clear': {},
  'rpc.admin.user.audit.append': {}
} as const;

async function mockRPC(subject: keyof typeof reply, payload: unknown) {
  calls.push({ subject, payload });
  if (subject === refused) throw new realNats.RpcError('refused');
  if (subject === replyOverride?.subject) return replyOverride.value;
  const value = reply[subject];
  return typeof value === 'function' ? value() : value;
}

mock.module('@bagel/kit/server/nats', () => ({ ...realNats, rpc: mockRPC, rpcReply: mockRPC }));

mock.module('#lib/server/commands-store', () => ({
  listModules: async () => [{ name: 'valorant', configs: commands }],
  upsertModule: async (uid: string, name: 'valorant', enabled: boolean, config: unknown) => {
    if (name === 'valorant') upserts.push({ uid, enabled, config });
    return { modules: [], synced: true };
  }
}));

const { GET: connect } = await import('../connect/+server');
const { GET: callback } = await import('./+server');
const { POST: unlink } = await import('../unlink/+server');

const origin = 'https://dashboard.test';
const locals = { session: { user_id: '7', sid: 'actor', impersonator_id: '5' }, locale: 'en' } as const;

function browserCookies() {
  const jar = new Map<string, string>();
  return {
    get: (name: string) => jar.get(name),
    set: mock<Cookies['set']>((name, value) => { jar.set(name, value); }),
    delete: mock((name: string) => { jar.delete(name); })
  };
}

async function redirectTo<Event>(
  run: (event: Event) => Response | Promise<Response>,
  event: { url: URL; locals: unknown; cookies: ReturnType<typeof browserCookies> }
): Promise<string> {
  try {
    await run(event as unknown as Event);
  } catch (thrown) {
    if (isRedirect(thrown)) return thrown.location;
    throw thrown;
  }
  throw new Error('handler did not redirect');
}

async function startFlow() {
  const cookies = browserCookies();
  const provider = new URL(await redirectTo(connect, { url: new URL('/riot/connect', origin), locals, cookies }));
  const url = new URL('/riot/callback', origin);
  url.searchParams.set('code', 'grant');
  url.searchParams.set('state', provider.searchParams.get('state')!);
  return { provider, cookies, url, locals };
}

beforeEach(() => {
  calls.length = 0;
  upserts.length = 0;
  linked = false;
  refused = '';
  replyOverride = undefined;
});

test('connect sends an authorization code request with one-use HTTPS cookie state', async () => {
  const { provider, cookies } = await startFlow();
  expect(provider.origin + provider.pathname).toBe('https://auth.riotgames.com/authorize');
  expect(Object.fromEntries(provider.searchParams)).toEqual({
    client_id: 'riot-client',
    redirect_uri: redirectUri,
    response_type: 'code',
    scope: 'openid',
    state: expect.any(String)
  });
  expect(cookies.set.mock.calls).toEqual([[
    'riot_oauth_state', expect.any(String),
    { path: '/', httpOnly: true, secure: true, sameSite: 'lax', maxAge: 600 }
  ]]);
});

test.each([
  { name: 'first link enables Valorant and preserves settings', previous: false },
  { name: 'relink preserves the enabled switch', previous: true }
])('$name', async ({ previous }) => {
  linked = previous;
  expect(await redirectTo(callback, await startFlow())).toBe('/modules/valorant?linked=1');
  expect(calls).toEqual([
    { subject: 'rpc.gossip.valorant.link', payload: { channel_id: '7', code: 'grant', redirect_uri: redirectUri } },
    { subject: 'rpc.modules.riot.link.status', payload: { user_id: '7' } },
    { subject: 'rpc.modules.riot.link.set', payload: { user_id: '7', ...account } },
    { subject: 'rpc.admin.user.audit.append', payload: expect.objectContaining({ action: 'dashboard:riot:link', target: '7', detail: '' }) }
  ]);
  expect(upserts).toEqual(previous ? [] : [{ uid: '7', enabled: true, config: commands }]);
});

test.each([
  { name: 'a mismatched state', slug: 'state', change: (url: URL) => url.searchParams.set('state', 'wrong') },
  { name: 'consent declined', slug: 'declined', change: (url: URL) => url.searchParams.set('error', 'access_denied') },
  { name: 'consent failed', slug: 'oauth', change: (url: URL) => url.searchParams.set('error', 'server_error') }
])('callback consumes the cookie and skips the exchange for $name', async ({ slug, change }) => {
  const flow = await startFlow();
  change(flow.url);
  expect(await redirectTo(callback, flow)).toBe(`/modules/valorant?e=${slug}`);
  expect(calls).toEqual([]);
  expect(await redirectTo(callback, flow)).toBe('/modules/valorant?e=state');
});

test.each([
  { session: { ...locals.session, delegate_of: '9', sections: ['modules'] }, path: '/modules/valorant?e=state' },
  { session: { ...locals.session, delegate_of: '9', sections: ['songqueue'] }, path: '/' },
  { session: { ...locals.session, sid: 'another-session' }, path: '/modules/valorant?e=state' },
  { session: null, path: '/login?next=/modules/valorant' }
])('callback enforces the original board, session and module access', async ({ session, path }) => {
  const flow = await startFlow();
  expect(await redirectTo(callback, { ...flow, locals: { ...locals, session } })).toBe(path);
  expect(calls).toEqual([]);
});

test.each([
  { subject: 'rpc.gossip.valorant.link', slug: 'oauth' },
  { subject: 'rpc.modules.riot.link.status', slug: 'store' },
  { subject: 'rpc.modules.riot.link.set', slug: 'store' }
])('callback reports refusal by $subject without enabling the module', async ({ subject, slug }) => {
  refused = subject;
  expect(await redirectTo(callback, await startFlow())).toBe(`/modules/valorant?e=${slug}`);
  expect(calls.at(-1)?.subject).toBe(subject);
  expect(upserts).toEqual([]);
});

test.each([
  { subject: 'rpc.gossip.valorant.link', slug: 'oauth', value: { ...account, error: 'riot refused' } },
  { subject: 'rpc.gossip.valorant.link', slug: 'oauth', value: { ...account, puuid: '' } },
  { subject: 'rpc.gossip.valorant.link', slug: 'oauth', value: { ...account, shard: undefined } },
  { subject: 'rpc.modules.riot.link.set', slug: 'store', value: { error: 'save refused' } }
])('callback rejects $value from $subject', async ({ subject, slug, value }) => {
  replyOverride = { subject, value };
  expect(await redirectTo(callback, await startFlow())).toBe(`/modules/valorant?e=${slug}`);
  expect(calls.at(-1)?.subject).toBe(subject);
  expect(upserts).toEqual([]);
});

test('unlink clears the Riot link and audits staff view-as', async () => {
  const actor = { ...locals, session: { ...locals.session, user_id: '9' } };
  const flow = { cookies: browserCookies(), url: new URL('/riot/unlink', origin), locals: actor };
  expect(await redirectTo(unlink, flow)).toBe('/modules/valorant');
  expect(calls).toEqual([
    { subject: 'rpc.modules.riot.link.clear', payload: { user_id: '9' } },
    { subject: 'rpc.admin.user.audit.append', payload: expect.objectContaining({ action: 'dashboard:riot:unlink', target: '9', detail: '' }) }
  ]);
  expect(upserts).toEqual([]);
});

test.each(['transport error', 'reply error'])('unlink handles a clear %s and cancels pending Riot consent', async (failure) => {
  const flow = await startFlow();
  const subject = 'rpc.modules.riot.link.clear';
  if (failure === 'transport error') refused = subject;
  else replyOverride = { subject, value: { error: 'clear refused' } };
  expect(await redirectTo(unlink, flow)).toBe('/modules/valorant?e=unlink');
  expect(calls).toEqual([{ subject, payload: { user_id: '7' } }]);
  expect(await redirectTo(callback, flow)).toBe('/modules/valorant?e=state');
});
