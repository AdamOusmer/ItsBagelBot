// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord bot-install callback. The guild id comes from the code exchange
// (see discord-oauth.ts), never from the query string.
import type { RequestHandler } from './$types';
import { logger } from '@bagel/shared/server/logger';
import {
  DISCORD_STATE_COOKIE,
  discordFail,
  exchangeInstallCode,
  requireDiscordActor
} from '$lib/server/discord-oauth';
import { isBoundElsewhere, readDiscord, saveDiscord, setupGuild, type DiscordConfig } from '$lib/server/discord-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { redirect, isRedirect } from '@sveltejs/kit';

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  const uid = requireDiscordActor(locals);

  const stored = cookies.get(DISCORD_STATE_COOKIE);
  cookies.delete(DISCORD_STATE_COOKIE, { path: '/', secure: url.protocol === 'https:' });

  const state = url.searchParams.get('state');
  const code = (url.searchParams.get('code') ?? '').trim();
  if (!stored) discordFail('state');
  if (!state) discordFail('state');
  if (stored !== state) discordFail('state');
  if (!code) discordFail('oauth');

  const guildId = await exchangeInstallCode(code).catch((err) => {
    logger.warn({ err }, '[discord-callback] code exchange failed');
    return '';
  });
  if (!guildId) discordFail('oauth');

  try {
    throw redirect(302, `/discord?${await connectGuild(locals, uid, guildId)}`);
  } catch (err) {
    if (isRedirect(err)) throw err;
    logger.error({ err }, '[discord-callback] persist failed');
    discordFail('setup');
  }
};

// connectGuild binds the guild, fills or adopts its layout, persists the
// module blob and returns the query string for the dashboard redirect.
async function connectGuild(locals: App.Locals, uid: string, guildId: string): Promise<string> {
  const view = await readDiscord(uid);
  const login = locals.session?.login ? locals.session.login : view.config.twitchLogin;
  const seeded = { ...view.config, guildId, twitchLogin: login };
  const result = await setupGuild(uid, guildId, seeded);
  if (isBoundElsewhere(result.error)) discordFail('bound');
  if (result.error) {
    logger.warn({ err: result.error }, '[discord-callback] setup failed; keeping the guild id');
  }
  await saveDiscord(uid, true, { ...configToKeep(result, seeded), twitchLogin: login });
  auditDashboardImpersonation(locals.session, 'discord:connect', guildId);
  if (result.refused) return 'connected=1&refused=1';
  return 'connected=1';
}

function configToKeep(
  result: { error: string; config: DiscordConfig },
  seeded: DiscordConfig
): DiscordConfig {
  if (result.error) return seeded;
  return result.config;
}
