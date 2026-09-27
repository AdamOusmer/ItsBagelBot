// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { isOAuthProtocolError } from '@bagel/kit/server/oauth';
import { botTwitch, botClientId, botScopes } from '$lib/server/oauth';
import { botTokenSet } from '$lib/server/bot-token';
import { env } from '$env/dynamic/private';

type BotIdentity = {
  claims: { sub: string; aud?: string | string[]; iss?: string; nonce?: string };
  configuredId: string;
  nonce: string;
};

function configuredBotId(): string {
  const botId = env.ADMIN_BOT_USER_ID?.trim();
  if (!botId) throw redirect(302, '/auth/bot/done?e=config');
  return botId;
}

function assertBotIdentity(identity: BotIdentity): void {
  const claims = identity.claims;
  const clientId = botClientId();
  const intendedAudience = Array.isArray(claims.aud)
    ? claims.aud.includes(clientId)
    : claims.aud === clientId;
  if (claims.nonce !== identity.nonce) {
    throw redirect(302, '/auth/bot/done?e=state');
  }
  if (!intendedAudience || claims.iss !== 'https://id.twitch.tv/oauth2') {
    throw redirect(302, '/auth/bot/done?e=state');
  }
  if (claims.sub !== identity.configuredId) {
    throw redirect(302, '/auth/bot/done?e=account');
  }
}

export const GET: RequestHandler = async ({ url, cookies }) => {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const stored = cookies.get('bot_oauth_state');
  const nonce = cookies.get('bot_oauth_nonce');
  cookies.delete('bot_oauth_state', { path: '/' });
  cookies.delete('bot_oauth_nonce', { path: '/' });

  if (!code || !nonce) {
    throw redirect(302, '/auth/bot/done?e=state');
  }
  if (!state || state !== stored) {
    throw redirect(302, '/auth/bot/done?e=state');
  }

  const botId = configuredBotId();

  try {
    const tokens = await botTwitch(url.origin).validateAuthorizationCode(code, nonce);
    assertBotIdentity({ claims: tokens.claims(), configuredId: botId, nonce });
    const granted = new Set(tokens.scopes());
    if (botScopes().some((scope) => !granted.has(scope))) {
      throw redirect(302, '/auth/bot/done?e=scope');
    }

    await botTokenSet(botId, tokens.accessToken(), tokens.refreshToken());
  } catch (e) {
    if (isOAuthProtocolError(e)) throw redirect(302, '/auth/bot/done?e=oauth');
    throw e;
  }

  throw redirect(302, '/auth/bot/done?ok=1');
};
