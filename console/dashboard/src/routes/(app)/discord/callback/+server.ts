// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord bot-install callback. The guild id comes from the code exchange
// (see discord-oauth.ts), never from the query string.
import type { RequestHandler } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { logger } from '@bagel/shared/server/logger';
import {
  DISCORD_STATE_COOKIE,
  discordFail,
  exchangeInstallCode,
  requireDiscordActor
} from '$lib/server/discord-oauth';
import {
  isBoundElsewhere,
  readDiscord,
  saveDiscord,
  setupGuild,
  type DiscordConfig,
  type DiscordGuildTarget
} from '$lib/server/discord-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { redirect, isRedirect } from '@sveltejs/kit';

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  const uid = requireDiscordActor(locals);
  const target = { userId: uid, guildId: await exchangedGuild(cookies, url) };
  try {
    throw redirect(302, `/discord?${await connectGuild(locals, target)}`);
  } catch (err) {
    if (isRedirect(err)) throw err;
    logger.error({ err }, '[discord-callback] persist failed');
    discordFail('setup');
  }
};

async function exchangedGuild(cookies: Cookies, url: URL): Promise<string> {
  const guildId = await exchangeInstallCode(takeInstallCode(cookies, url)).catch((err) => {
    logger.warn({ err }, '[discord-callback] code exchange failed');
    return '';
  });
  if (!guildId) discordFail('oauth');
  return guildId;
}

function takeInstallCode(cookies: Cookies, url: URL): string {
  const stored = cookies.get(DISCORD_STATE_COOKIE);
  cookies.delete(DISCORD_STATE_COOKIE, { path: '/', secure: url.protocol === 'https:' });
  const state = url.searchParams.get('state');
  const code = (url.searchParams.get('code') ?? '').trim();
  if (!stored) discordFail('state');
  if (!state) discordFail('state');
  if (stored !== state) discordFail('state');
  if (!code) discordFail('oauth');
  return code;
}

// connectGuild binds the guild, fills or adopts its layout, persists the
// module blob and returns the query string for the dashboard redirect.
async function connectGuild(locals: App.Locals, target: DiscordGuildTarget): Promise<string> {
  const view = await readDiscord(target);
  const login = locals.session?.login ? locals.session.login : view.config.twitchLogin;
  const seeded = { ...view.config, guildId: target.guildId, twitchLogin: login };
  const result = await setupGuild(target, seeded);
  if (isBoundElsewhere(result)) discordFail('bound');
  if (result.error) {
    logger.warn({ err: result.error }, '[discord-callback] setup failed; keeping the guild id');
  }
  await saveDiscord({ userId: target.userId, enabled: true, config: { ...configToKeep(result, seeded), twitchLogin: login } });
  auditDashboardImpersonation(locals.session, 'discord:connect', target.guildId);
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
