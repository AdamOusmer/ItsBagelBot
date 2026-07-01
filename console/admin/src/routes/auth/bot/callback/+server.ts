import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { decodeIdToken, OAuth2RequestError } from 'arctic';
import { botTwitch, botClientId } from '$lib/server/oauth';
import { tokenSet } from '$lib/server/services';
import { env } from '$env/dynamic/private';

// Twitch redirects the bot account's browser here after consent. Same cookie
// state check as the operator callback; no admin session is minted and no
// adminCheck runs (the bot account is not staff). The token is stored under the
// bot's id, optionally pinned by ADMIN_BOT_USER_ID.
export const GET: RequestHandler = async ({ url, cookies }) => {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const stored = cookies.get('bot_oauth_state');
  cookies.delete('bot_oauth_state', { path: '/' });

  if (!code || !state || !stored || state !== stored) {
    throw redirect(302, '/auth/bot/done?e=state');
  }

  try {
    const tokens = await botTwitch(url.origin).validateAuthorizationCode(code);
    const claims = decodeIdToken(tokens.idToken()!) as {
      sub: string;
      aud?: string | string[];
      iss?: string;
    };

    const clientId = botClientId();
    const audOk = Array.isArray(claims.aud)
      ? claims.aud.includes(clientId)
      : claims.aud === clientId;
    const issOk = claims.iss === 'https://id.twitch.tv/oauth2';
    if (!audOk || !issOk) throw redirect(302, '/auth/bot/done?e=state');

    // Guard: if the bot account id is pinned, refuse any other account's token.
    const botId = env.ADMIN_BOT_USER_ID ?? '';
    if (botId && claims.sub !== botId) throw redirect(302, '/auth/bot/done?e=account');

    await tokenSet(claims.sub, tokens.accessToken(), tokens.refreshToken());
  } catch (e) {
    if (e instanceof OAuth2RequestError) throw redirect(302, '/auth/bot/done?e=oauth');
    throw e;
  }

  throw redirect(302, '/auth/bot/done?ok=1');
};
