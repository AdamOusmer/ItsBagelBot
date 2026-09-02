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
import { isBoundElsewhere, readDiscord, saveDiscord, setupGuild } from '$lib/server/discord-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { redirect, isRedirect } from '@sveltejs/kit';

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  const uid = requireDiscordActor(locals);

  const stored = cookies.get(DISCORD_STATE_COOKIE);
  cookies.delete(DISCORD_STATE_COOKIE, { path: '/', secure: url.protocol === 'https:' });

  const state = url.searchParams.get('state');
  const code = (url.searchParams.get('code') ?? '').trim();
  if (!stored || !state || stored !== state) discordFail('state');
  if (!code) discordFail('oauth');

  const guildId = await exchangeInstallCode(code).catch((err) => {
    logger.warn({ err }, '[discord-callback] code exchange failed');
    return '';
  });
  if (!guildId) discordFail('oauth');

  const login = locals.session?.login ?? '';
  try {
    const view = await readDiscord(uid);
    const seeded = { ...view.config, guildId, twitchLogin: login || view.config.twitchLogin };
    const result = await setupGuild(uid, guildId, seeded);
    if (isBoundElsewhere(result.error)) discordFail('bound');
    if (result.error) {
      logger.warn({ err: result.error }, '[discord-callback] setup failed; keeping the guild id');
    }
    await saveDiscord(uid, true, {
      ...(result.error ? seeded : result.config),
      twitchLogin: login || view.config.twitchLogin
    });
    auditDashboardImpersonation(locals.session, 'discord:connect', guildId);
    const q = result.refused ? 'connected=1&refused=1' : 'connected=1';
    throw redirect(302, `/discord?${q}`);
  } catch (err) {
    if (isRedirect(err)) throw err;
    logger.error({ err }, '[discord-callback] persist failed');
    discordFail('setup');
  }
};
