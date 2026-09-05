// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Step two of two: the bot-install callback. The guild id comes from the code
// exchange (see discord-oauth.ts), never from the query string: the query
// guild_id is caller-supplied, and trusting it let any signed-in user bind
// (and fill) someone else's server. Only a member who may add bots to a guild
// can obtain a code for it, which is the ownership proof outgress relies on.
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
  blankDiscordConfig,
  persistSetup,
  pinnedRolesOf,
  readDiscord,
  readGuildConfig,
  saveDiscordModule,
  setupGuild,
  type DiscordGuildTarget
} from '$lib/server/discord-store';
import { alertOff } from '@bagel/shared';
import { auditDashboardImpersonation } from '$lib/server/services';
import { redirect, isRedirect } from '@sveltejs/kit';

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  const uid = requireDiscordActor(locals);
  const target = { userId: uid, guildId: await exchangedGuild(cookies, url) };
  try {
    throw redirect(302, `/discord/${target.guildId}?${await connectGuild(locals, target)}`);
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

// connectGuild binds the guild, fills or adopts its layout, writes the guild's
// own config row, and returns the query string for the dashboard redirect.
// The module row is only touched to turn Discord on: a broadcaster adding a
// second server must not have their first one's settings rewritten.
async function connectGuild(locals: App.Locals, target: DiscordGuildTarget): Promise<string> {
  const view = await readDiscord({ userId: target.userId }).catch(() => null);
  const login = locals.session?.login ? locals.session.login : (view?.twitchLogin ?? '');
  const row = await readGuildConfig(target).catch(() => null);
  const seeded = { ...(row?.config ?? blankDiscordConfig()), guildId: target.guildId, twitchLogin: login };
  const result = await setupGuild(
    { ...target, subscribers: alertOff(seeded.subscribersEnabled), pinnedRoles: pinnedRolesOf(seeded) },
    seeded
  );
  // The refusal is named by its code now, not by matching outgress's English.
  if (result.code === 'bound_elsewhere') discordFail('bound_elsewhere');
  if (result.error) {
    logger.warn({ err: result.error }, '[discord-callback] setup failed; keeping the binding');
  }
  await persistSetup(target, { ...(result.error ? seeded : result.config), twitchLogin: login });
  await saveDiscordModule({ userId: target.userId, enabled: true, twitchLogin: login });
  auditDashboardImpersonation(locals.session, 'discord:connect', target.guildId);
  if (result.refused) return 'connected=1&refused=1';
  return 'connected=1';
}
