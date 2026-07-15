import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { decodeIdToken, OAuth2RequestError } from 'arctic';
import { botTwitch, botClientId } from '$lib/server/oauth';
import { tokenSet } from '$lib/server/services';
import { env } from '$env/dynamic/private';

type BotIdentity = {
  idToken: string;
  configuredId: string;
};

function configuredBotId(): string {
  const botId = env.ADMIN_BOT_USER_ID?.trim();
  if (!botId) throw redirect(302, '/auth/bot/done?e=config');
  return botId;
}

function assertBotIdentity(identity: BotIdentity): void {
  const claims = decodeIdToken(identity.idToken) as {
    sub: string;
    aud?: string | string[];
    iss?: string;
  };
  const clientId = botClientId();
  const intendedAudience = Array.isArray(claims.aud)
    ? claims.aud.includes(clientId)
    : claims.aud === clientId;
  if (!intendedAudience || claims.iss !== 'https://id.twitch.tv/oauth2') {
    throw redirect(302, '/auth/bot/done?e=state');
  }
  if (claims.sub !== identity.configuredId) {
    throw redirect(302, '/auth/bot/done?e=account');
  }
}

// Twitch redirects the bot account's browser here after consent. Same cookie
// state check as the operator callback; no admin session is minted and no
// adminCheck runs (the bot account is not staff). The token is stored under the
// configured bot id. The callback refuses to exchange or store a token unless
// ADMIN_BOT_USER_ID is set.
export const GET: RequestHandler = async ({ url, cookies }) => {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const stored = cookies.get('bot_oauth_state');
  cookies.delete('bot_oauth_state', { path: '/' });

  if (!code || !state || !stored || state !== stored) {
    throw redirect(302, '/auth/bot/done?e=state');
  }

  const botId = configuredBotId();

  try {
    const tokens = await botTwitch(url.origin).validateAuthorizationCode(code);
    assertBotIdentity({ idToken: tokens.idToken()!, configuredId: botId });

    await tokenSet(botId, tokens.accessToken(), tokens.refreshToken());
  } catch (e) {
    if (e instanceof OAuth2RequestError) throw redirect(302, '/auth/bot/done?e=oauth');
    throw e;
  }

  throw redirect(302, '/auth/bot/done?ok=1');
};
