// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { isOAuthProtocolError } from '@bagel/kit/server/oauth';
import { logger } from '@bagel/kit/server/logger';
import { botTwitch, botClientId, botScopes } from '$lib/server/oauth';
import { botTokenSet, type TokenGrant } from '$lib/server/services';
import { env } from '$env/dynamic/private';

type BotIdentity = {
  claims: { sub: string; aud?: string | string[]; iss?: string; nonce?: string };
  configuredId: string;
  nonce: string;
};

function configuredBotId(): string {
  const botId = env.TWITCH_BOT_USER_ID?.trim();
  if (!botId) throw redirect(302, '/auth/bot/done?e=config');
  return botId;
}

function intendedBotAudience(audience: BotIdentity['claims']['aud']): boolean {
  const clientId = botClientId();
  return Array.isArray(audience) ? audience.includes(clientId) : audience === clientId;
}

function assertBotIdentity(identity: BotIdentity): void {
  const claims = identity.claims;
  if (claims.nonce !== identity.nonce) {
    throw redirect(302, '/auth/bot/done?e=state');
  }
  if (!intendedBotAudience(claims.aud) || claims.iss !== 'https://id.twitch.tv/oauth2') {
    throw redirect(302, '/auth/bot/done?e=state');
  }
  if (claims.sub !== identity.configuredId) {
    throw redirect(302, '/auth/bot/done?e=account');
  }
}

async function exchangeBotGrant(origin: string, code: string, nonce: string, botId: string): Promise<TokenGrant> {
  try {
    const tokens = await botTwitch(origin).validateAuthorizationCode(code, nonce);
    assertBotIdentity({ claims: tokens.claims(), configuredId: botId, nonce });
    const granted = new Set(tokens.scopes());
    if (botScopes().some((scope) => !granted.has(scope))) {
      throw redirect(302, '/auth/bot/done?e=scope');
    }
    return { accessToken: tokens.accessToken(), refreshToken: tokens.refreshToken() };
  } catch (e) {
    if (isOAuthProtocolError(e)) throw redirect(302, '/auth/bot/done?e=oauth');
    throw e;
  }
}

async function storeBotGrant(botId: string, grant: TokenGrant): Promise<void> {
  try {
    await botTokenSet({ actorId: botId, userId: botId }, grant);
  } catch (err) {
    logger.error({ err, botId }, 'bot token save failed');
    throw redirect(302, '/auth/bot/done?e=save');
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
  const grant = await exchangeBotGrant(url.origin, code, nonce, botId);
  await storeBotGrant(botId, grant);

  throw redirect(302, '/auth/bot/done?ok=1');
};
