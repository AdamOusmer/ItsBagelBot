// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, describe, expect, mock, spyOn, test } from 'bun:test';
import { logger } from '@bagel/kit/server/logger';

const privateEnv: Record<string, string | undefined> = {};
const claims: { sub: string; aud: string | string[]; iss: string; nonce: string } = {
  sub: 'configured-bot',
  aud: 'bot-client',
  iss: 'https://id.twitch.tv/oauth2',
  nonce: 'nonce'
};
const GRANTED = ['openid', 'moderator:read:chatters'];
let grantedScopes = GRANTED;
const validateAuthorizationCode = mock(async () => ({
  claims: () => ({ ...claims }),
  accessToken: () => 'access-token',
  refreshToken: () => 'refresh-token',
  scopes: () => grantedScopes
}));
const botTwitch = mock(() => ({ validateAuthorizationCode, createAuthorizationURL: () => new URL('https://id.twitch.tv/oauth2/authorize') }));
let botTokenSetFailure: Error | null = null;
const botTokenSet = mock(async (_actor: unknown, _grant: unknown) => {
  if (botTokenSetFailure) throw botTokenSetFailure;
  return { present: true };
});
const loggerError = spyOn(logger, 'error').mockImplementation(() => {});

mock.module('$env/dynamic/private', () => ({ env: privateEnv }));
mock.module('$lib/server/oauth', () => ({ botClientId: () => 'bot-client', botScopes: () => GRANTED, botTwitch }));
mock.module('$lib/server/services', () => ({ botTokenSet }));

const { GET: callbackGET } = await import('../src/routes/auth/bot/callback/+server');
const { GET: loginGET } = await import('../src/routes/auth/bot/login/+server');

type Cookie = 'bot_oauth_state' | 'bot_oauth_nonce';

function callbackEvent(missingCookie?: Cookie, state = 'state') {
  const cookies: Record<string, string | undefined> = { bot_oauth_state: 'state', bot_oauth_nonce: 'nonce' };
  if (missingCookie) cookies[missingCookie] = undefined;
  return {
    url: new URL(`https://admin.example/auth/bot/callback?code=code&state=${state}`),
    locals: { session: null },
    cookies: { get: (name: string) => cookies[name], delete: mock((_name: string, _options: unknown) => {}) }
  };
}

async function redirectedTo(request: () => unknown): Promise<unknown> {
  try {
    await request();
  } catch (error) {
    return error;
  }
  throw new Error('Expected the bot authorization route to redirect');
}

const callback = (event = callbackEvent()) => redirectedTo(() => callbackGET(event as unknown as Parameters<typeof callbackGET>[0]));
const redirectTo = (location: string) => ({ status: 302, location });

beforeEach(() => {
  for (const key of Object.keys(privateEnv)) delete privateEnv[key];
  privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
  grantedScopes = GRANTED;
  Object.assign(claims, { sub: 'configured-bot', aud: 'bot-client', iss: 'https://id.twitch.tv/oauth2', nonce: 'nonce' });
  botTwitch.mockClear();
  validateAuthorizationCode.mockClear();
  botTokenSet.mockClear();
  botTokenSetFailure = null;
  loggerError.mockClear();
});

type Refusal = { name: string; setup: () => void; event?: () => ReturnType<typeof callbackEvent>; location: string; exchanged: boolean };

const refusals: Refusal[] = [
  { name: 'rejects an unset bot id before exchanging or storing a token', setup: () => delete privateEnv.TWITCH_BOT_USER_ID, location: '/auth/bot/done?e=config', exchanged: false },
  { name: 'rejects a different Twitch account without storing its token', setup: () => (claims.sub = 'other-account'), location: '/auth/bot/done?e=account', exchanged: true },
  { name: 'refuses a token without chatter access instead of reporting authorization success', setup: () => (grantedScopes = ['openid']), location: '/auth/bot/done?e=scope', exchanged: true },
  { name: 'refuses a response that does not report its granted scopes', setup: () => (grantedScopes = []), location: '/auth/bot/done?e=scope', exchanged: true },
  { name: 'rejects absent bot_oauth_state before exchanging a code', setup: () => {}, event: () => callbackEvent('bot_oauth_state'), location: '/auth/bot/done?e=state', exchanged: false },
  { name: 'rejects absent bot_oauth_nonce before exchanging a code', setup: () => {}, event: () => callbackEvent('bot_oauth_nonce'), location: '/auth/bot/done?e=state', exchanged: false },
  { name: 'rejects a mismatched OAuth state before exchanging a code', setup: () => {}, event: () => callbackEvent(undefined, 'other-state'), location: '/auth/bot/done?e=state', exchanged: false },
  { name: 'rejects mismatched aud without saving a token', setup: () => (claims.aud = 'mismatch'), location: '/auth/bot/done?e=state', exchanged: true },
  { name: 'rejects mismatched iss without saving a token', setup: () => (claims.iss = 'mismatch'), location: '/auth/bot/done?e=state', exchanged: true },
  { name: 'rejects mismatched nonce without saving a token', setup: () => (claims.nonce = 'mismatch'), location: '/auth/bot/done?e=state', exchanged: true },
  { name: 'rejects an ID token audience array without the bot client', setup: () => (claims.aud = ['other-client']), location: '/auth/bot/done?e=state', exchanged: true }
];

describe('bot OAuth callback account pinning', () => {
  test.each(refusals)('$name', async ({ setup, event, location, exchanged }) => {
    setup();
    const request = event?.() ?? callbackEvent();
    expect(await callback(request)).toMatchObject(redirectTo(location));
    expect(validateAuthorizationCode).toHaveBeenCalledTimes(exchanged ? 1 : 0);
    expect(botTokenSet).not.toHaveBeenCalled();
    expect(request.cookies.delete.mock.calls).toEqual([['bot_oauth_state', { path: '/' }], ['bot_oauth_nonce', { path: '/' }]]);
  });

  test('stores the configured bot token without a console session', async () => {
    privateEnv.TWITCH_BOT_USER_ID = ' configured-bot ';

    expect(await callback()).toMatchObject(redirectTo('/auth/bot/done?ok=1'));
    expect(validateAuthorizationCode).toHaveBeenCalledWith('code', 'nonce');
    expect(botTokenSet.mock.calls).toEqual([
      [{ actorId: 'configured-bot', userId: 'configured-bot' }, { accessToken: 'access-token', refreshToken: 'refresh-token' }]
    ]);
  });

  test('accepts an ID token audience array containing the bot client', async () => {
    claims.aud = ['other-client', 'bot-client'];

    expect(await callback()).toMatchObject(redirectTo('/auth/bot/done?ok=1'));
    expect(botTokenSet).toHaveBeenCalledTimes(1);
  });

  test.each([
    ['a users service refusal', Object.assign(new Error('configured bot identity required'), { code: 'forbidden' })],
    ['no users service responders', new Error('503')]
  ] as const)('reports %s as a save failure instead of a server error', async (_name, failure) => {
    botTokenSetFailure = failure;

    expect(await callback()).toMatchObject(redirectTo('/auth/bot/done?e=save'));
    expect(botTokenSet).toHaveBeenCalledTimes(1);
    expect(loggerError).toHaveBeenCalledWith(expect.objectContaining({ err: failure, botId: 'configured-bot' }), 'bot token save failed');
  });
});

describe('bot OAuth login', () => {
  const login = (cookies: { set: ReturnType<typeof mock> }) =>
    redirectedTo(() => loginGET({ url: new URL('https://admin.example/auth/bot/login'), cookies, locals: { session: null } } as unknown as Parameters<typeof loginGET>[0]));

  test('refuses to start bot authorization when no bot id is configured', async () => {
    delete privateEnv.TWITCH_BOT_USER_ID;
    const set = mock(() => {});

    expect(await login({ set })).toMatchObject(redirectTo('/auth/bot/done?e=config'));
    expect(botTwitch).not.toHaveBeenCalled();
    expect(set).not.toHaveBeenCalled();
  });

  test('starts consent without a staff session and binds a nonce cookie', async () => {
    const set = mock(() => {});

    const started = (await login({ set })) as { location: string };
    const auth = new URL(started.location);
    const nonce = auth.searchParams.get('nonce');
    expect(auth.searchParams.get('force_verify')).toBe('true');
    expect(nonce).toBeTruthy();
    expect(set).toHaveBeenCalledWith('bot_oauth_nonce', nonce, expect.objectContaining({ httpOnly: true, secure: true, sameSite: 'lax' }));
  });
});
