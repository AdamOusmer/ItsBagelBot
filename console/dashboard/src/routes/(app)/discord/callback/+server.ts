// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Discord bot-install callback. Discord redirects with guild_id after the
// owner adds Bagel. We do not exchange the code: the bot token is fleet-wide.
import type { RequestHandler } from './$types';
import { logger } from '@bagel/shared/server/logger';
import {
  DISCORD_STATE_COOKIE,
  discordFail,
  requireDiscordActor
} from '$lib/server/discord-oauth';
import { readDiscord, saveDiscord, setupGuild } from '$lib/server/discord-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { redirect, isRedirect } from '@sveltejs/kit';

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  const uid = requireDiscordActor(locals);

  const stored = cookies.get(DISCORD_STATE_COOKIE);
  cookies.delete(DISCORD_STATE_COOKIE, { path: '/' });

  const state = url.searchParams.get('state');
  const guildId = (url.searchParams.get('guild_id') ?? '').trim();
  if (!stored || !state || stored !== state) discordFail('state');
  if (!guildId) discordFail('oauth');

  const login = locals.session?.login ?? '';
  try {
    const view = await readDiscord(uid);
    const seeded = { ...view.config, guildId, twitchLogin: login || view.config.twitchLogin };
    const result = await setupGuild(uid, guildId, seeded);
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
