// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, describe, expect, mock, test } from 'bun:test';

const privateEnv: Record<string, string | undefined> = {};
const claims: { sub: string; aud: string; iss: string } = {
  sub: 'configured-bot',
  aud: 'bot-client',
  iss: 'https://id.twitch.tv/oauth2'
};
const validateAuthorizationCode = mock(async () => ({
  idToken: () => 'unused.test.token',
  accessToken: () => 'access-token',
  refreshToken: () => 'refresh-token'
}));
const createAuthorizationURL = mock(() => new URL('https://id.twitch.tv/oauth2/authorize'));
const botTwitch = mock(() => ({ validateAuthorizationCode, createAuthorizationURL }));
const tokenSet = mock(async () => undefined);

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
  botScopes: () => ['openid'],
  botTwitch
}));
mock.module('$lib/server/services', () => ({ tokenSet }));
mock.module('arctic', () => ({
  decodeIdToken: () => claims,
  generateState: () => 'generated-state',
  OAuth2RequestError: class OAuth2RequestError extends Error {}
}));
mock.module('@sveltejs/kit', () => ({
  redirect: (status: number, location: string) => new TestRedirect(status, location)
}));

const { GET: callbackGET } = await import('../src/routes/auth/bot/callback/+server');
const { GET: loginGET } = await import('../src/routes/auth/bot/login/+server');

function callbackEvent() {
  return {
    url: new URL('https://admin.example/auth/bot/callback?code=code&state=state'),
    cookies: {
      get: (name: string) => (name === 'bot_oauth_state' ? 'state' : undefined),
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
  claims.sub = 'configured-bot';
  claims.aud = 'bot-client';
  claims.iss = 'https://id.twitch.tv/oauth2';
  botTwitch.mockClear();
  validateAuthorizationCode.mockClear();
  createAuthorizationURL.mockClear();
  tokenSet.mockClear();
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
    expect(tokenSet).not.toHaveBeenCalled();
  });

  test('rejects a different Twitch account without storing its token', async () => {
    privateEnv.ADMIN_BOT_USER_ID = 'configured-bot';
    claims.sub = 'other-account';

    await expectRedirectLocation(
      () => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]),
      '/auth/bot/done?e=account'
    );

    expect(validateAuthorizationCode).toHaveBeenCalledTimes(1);
    expect(tokenSet).not.toHaveBeenCalled();
  });

  test('stores the token only under the matching configured account', async () => {
    privateEnv.ADMIN_BOT_USER_ID = ' configured-bot ';

    await expectRedirectLocation(
      () => callbackGET(callbackEvent() as Parameters<typeof callbackGET>[0]),
      '/auth/bot/done?ok=1'
    );

    expect(tokenSet).toHaveBeenCalledTimes(1);
    expect(tokenSet).toHaveBeenCalledWith('configured-bot', 'access-token', 'refresh-token');
  });
});
