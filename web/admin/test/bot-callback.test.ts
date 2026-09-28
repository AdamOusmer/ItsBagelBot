// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, describe, expect, mock, test } from 'bun:test';

const privateEnv: Record<string, string | undefined> = {};
const claims: { sub: string; aud: string | string[]; iss: string; nonce: string } = {
  sub: 'configured-bot',
  aud: 'bot-client',
  iss: 'https://id.twitch.tv/oauth2',
  nonce: 'nonce'
};
let grantedScopes = ['openid', 'moderator:read:chatters'];
const validateAuthorizationCode = mock(async () => ({
  claims: () => ({ ...claims }),
  accessToken: () => 'access-token',
  refreshToken: () => 'refresh-token',
  scopes: () => grantedScopes
}));
const createAuthorizationURL = mock(() => new URL('https://id.twitch.tv/oauth2/authorize'));
const botTwitch = mock(() => ({ validateAuthorizationCode, createAuthorizationURL }));
let botTokenSetFailure: Error | null = null;
const botTokenSet = mock(async () => {
  if (botTokenSetFailure) throw botTokenSetFailure;
  return { present: true };
});
const loggerError = mock(() => {});

class TestRedirect extends Error {
  constructor(
    readonly status: number,
    readonly location: string
  ) {
    super(`Redirect to ${location}`);
  }
}

mock.module('$env/dynamic/private', () => ({ env: privateEnv }));
mock.module('$lib/server/oauth', () => ({
  botClientId: () => 'bot-client',
  botScopes: () => ['openid', 'moderator:read:chatters'],
  botTwitch
}));
mock.module('$lib/server/services', () => ({ botTokenSet }));
mock.module('@bagel/kit/server/logger', () => ({ logger: { error: loggerError } }));
mock.module('@sveltejs/kit', () => ({
  redirect: (status: number, location: string) => new TestRedirect(status, location)
}));

const { GET: callbackGET } = await import('../src/routes/auth/bot/callback/+server');
const { GET: loginGET } = await import('../src/routes/auth/bot/login/+server');

function callbackEvent() {
  return {
    url: new URL('https://admin.example/auth/bot/callback?code=code&state=state'),
    locals: { session: null },
    cookies: {
      get: (name: string) => (name === 'bot_oauth_state' ? 'state' : name === 'bot_oauth_nonce' ? 'nonce' : undefined),
      delete: mock(() => {})
    }
  };
}

async function expectRedirectLocation(request: () => unknown, location: string): Promise<void> {
  try {
    await request();
    throw new Error('Expected the bot authorization route to redirect');
  } catch (error) {
    expect(error).toBeInstanceOf(TestRedirect);
    expect(error).toMatchObject({ status: 302, location });
  }
}

beforeEach(() => {
  for (const key of Object.keys(privateEnv)) delete privateEnv[key];
  grantedScopes = ['openid', 'moderator:read:chatters'];
  claims.sub = 'configured-bot';
  claims.aud = 'bot-client';
  claims.iss = 'https://id.twitch.tv/oauth2';
  claims.nonce = 'nonce';
  botTwitch.mockClear();
  validateAuthorizationCode.mockClear();
  createAuthorizationURL.mockClear();
  botTokenSet.mockClear();
  botTokenSetFailure = null;
  loggerError.mockClear();
});

describe('bot OAuth callback account pinning', () => {
  test('refuses to start bot authorization when no bot id is configured', async () => {
    const setCookie = mock(() => {});

    await expectRedirectLocation(
      () =>
        loginGET({
          url: new URL('https://admin.example/auth/bot/login'),
          cookies: { set: setCookie }
        } as Parameters<typeof loginGET>[0]),
      '/auth/bot/done?e=config'
    );

    expect(botTwitch).not.toHaveBeenCalled();
    expect(setCookie).not.toHaveBeenCalled();
  });

  test('rejects an unset bot id before exchanging or storing a token', async () => {
    await expectRedirectLocation(
      () => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]),
      '/auth/bot/done?e=config'
    );

    expect(botTwitch).not.toHaveBeenCalled();
    expect(validateAuthorizationCode).not.toHaveBeenCalled();
    expect(botTokenSet).not.toHaveBeenCalled();
  });

  test('rejects a different Twitch account without storing its token', async () => {
    privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
    claims.sub = 'other-account';

    await expectRedirectLocation(
      () => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]),
      '/auth/bot/done?e=account'
    );

    expect(validateAuthorizationCode).toHaveBeenCalledTimes(1);
    expect(botTokenSet).not.toHaveBeenCalled();
  });

  test('refuses a token without chatter access instead of reporting authorization success', async () => {
    privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
    grantedScopes = ['openid'];
    await expectRedirectLocation(
      () => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]),
      '/auth/bot/done?e=scope'
    );
    expect(botTokenSet).not.toHaveBeenCalled();
  });

  test('refuses a response that does not report its granted scopes', async () => {
    privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
    grantedScopes = [];
    await expectRedirectLocation(
      () => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]),
      '/auth/bot/done?e=scope'
    );
    expect(botTokenSet).not.toHaveBeenCalled();
  });

  test('stores the configured bot token without a console session', async () => {
    privateEnv.TWITCH_BOT_USER_ID = ' configured-bot ';

    await expectRedirectLocation(
      () => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]),
      '/auth/bot/done?ok=1'
    );

    expect(validateAuthorizationCode).toHaveBeenCalledWith('code', 'nonce');
    expect(botTokenSet).toHaveBeenCalledTimes(1);
    expect(botTokenSet).toHaveBeenCalledWith(
      { actorId: 'configured-bot', userId: 'configured-bot' },
      'access-token',
      'refresh-token'
    );
  });

  for (const [name, failure] of [
    ['a users service refusal', Object.assign(new Error('configured bot identity required'), { code: 'forbidden' })],
    ['no users service responders', new Error('503')]
  ] as const) {
    test(`reports ${name} as a save failure instead of a server error`, async () => {
      privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
      botTokenSetFailure = failure;

      await expectRedirectLocation(
        () => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]),
        '/auth/bot/done?e=save'
      );

      expect(botTokenSet).toHaveBeenCalledTimes(1);
      expect(loggerError).toHaveBeenCalledWith(
        expect.objectContaining({ err: failure, botId: 'configured-bot' }),
        'bot token save failed'
      );
    });
  }

  test('starts consent without a staff session and binds a nonce cookie', async () => {
    privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
    const set = mock(() => {});
    try {
      loginGET({ url: new URL('https://admin.example/auth/bot/login'), cookies: { set }, locals: { session: null } } as unknown as Parameters<typeof loginGET>[0]);
    } catch (error) {
      expect(error).toBeInstanceOf(TestRedirect);
      const auth = new URL((error as TestRedirect).location);
      expect(auth.searchParams.get('force_verify')).toBe('true');
      const nonce = auth.searchParams.get('nonce');
      expect(nonce).toBeTruthy();
      expect(set).toHaveBeenCalledWith('bot_oauth_nonce', nonce, expect.objectContaining({ httpOnly: true, secure: true, sameSite: 'lax' }));
    }
  });

  for (const missingCookie of ['bot_oauth_state', 'bot_oauth_nonce']) {
    test(`rejects absent ${missingCookie} before exchanging a code`, async () => {
      privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
      const event = callbackEvent();
      const get = event.cookies.get;
      event.cookies.get = (name) => name === missingCookie ? undefined : get(name);
      await expectRedirectLocation(() => callbackGET(event as Parameters<typeof callbackGET>[0]), '/auth/bot/done?e=state');
      expect(validateAuthorizationCode).not.toHaveBeenCalled();
      expect(botTokenSet).not.toHaveBeenCalled();
      expect(event.cookies.delete).toHaveBeenCalledWith('bot_oauth_state', { path: '/' });
      expect(event.cookies.delete).toHaveBeenCalledWith('bot_oauth_nonce', { path: '/' });
    });
  }

  test('rejects a mismatched OAuth state before exchanging a code', async () => {
    privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
    const event = callbackEvent();
    event.url.searchParams.set('state', 'other-state');
    await expectRedirectLocation(() => callbackGET(event as Parameters<typeof callbackGET>[0]), '/auth/bot/done?e=state');
    expect(validateAuthorizationCode).not.toHaveBeenCalled();
    expect(botTokenSet).not.toHaveBeenCalled();
  });

  for (const field of ['aud', 'iss', 'nonce'] as const) {
    test(`rejects mismatched ${field} without saving a token`, async () => {
      privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
      claims[field] = 'mismatch';
      await expectRedirectLocation(() => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]), '/auth/bot/done?e=state');
      expect(botTokenSet).not.toHaveBeenCalled();
    });
  }


  test('accepts an ID token audience array containing the bot client', async () => {
    privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
    claims.aud = ['other-client', 'bot-client'];
    await expectRedirectLocation(() => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]), '/auth/bot/done?ok=1');
    expect(botTokenSet).toHaveBeenCalledTimes(1);
  });

  test('rejects an ID token audience array without the bot client', async () => {
    privateEnv.TWITCH_BOT_USER_ID = 'configured-bot';
    claims.aud = ['other-client'];
    await expectRedirectLocation(() => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]), '/auth/bot/done?e=state');
    expect(botTokenSet).not.toHaveBeenCalled();
  });

});
