// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { ResponseBodyError } from '@bagel/shared/server/oauth';
import { botTwitch, botClientId } from '$lib/server/oauth';
import { tokenSet } from '$lib/server/services';
import { requireRole } from '$lib/server/access';
import { env } from '$env/dynamic/private';

type BotIdentity = {
  claims: { sub: string; aud?: string | string[]; iss?: string };
  configuredId: string;
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
  if (!intendedAudience || claims.iss !== 'https://id.twitch.tv/oauth2') {
    throw redirect(302, '/auth/bot/done?e=state');
  }
  if (claims.sub !== identity.configuredId) {
    throw redirect(302, '/auth/bot/done?e=account');
  }
}

// Twitch redirects the bot account's browser here after consent. Same cookie
// state check as the operator callback. No admin session is minted for the bot
// account (it is not staff); the OWNER's session in this browser authorizes the
// write, and its id is what the users service checks against its staff table.
// The token is stored under the configured bot id. The callback refuses to
// exchange or store a token unless ADMIN_BOT_USER_ID is set.
export const GET: RequestHandler = async ({ url, cookies, locals }) => {
  // hooks.server.ts already refused a non-owner here; resolving again gives
  // the actor id the users service needs to authorize token_set, and keeps
  // this endpoint correct on its own if the hook's prefix list ever changes.
  const owner = await requireRole({ locals }, 'bot.token');
  if (!owner) throw redirect(302, '/auth/bot/done?e=denied');

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
    assertBotIdentity({ claims: tokens.claims(), configuredId: botId });

    await tokenSet(owner.id, botId, tokens.accessToken(), tokens.refreshToken());
  } catch (e) {
    if (e instanceof ResponseBodyError) throw redirect(302, '/auth/bot/done?e=oauth');
    throw e;
  }

  throw redirect(302, '/auth/bot/done?ok=1');
};
