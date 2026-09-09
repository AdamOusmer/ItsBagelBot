// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Step two of two: the bot-install callback. The guild id comes from the code
// exchange (see discord-oauth.ts), never from the query string: the query
// guild_id is caller-supplied, and trusting it let any signed-in user bind
// (and fill) someone else's server. Only a member who may add bots to a guild
// can obtain a code for it, which is the ownership proof outgress relies on.
import type { RequestHandler } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { logger } from '@bagel/kit/server/logger';
import {
  DISCORD_ERROR_SLUGS,
  DISCORD_INSTALL_LEG,
  discordFail,
  discordStateOK,
  exchangeInstallCode,
  rememberBoundElsewhere,
  requireDiscordActor,
  type DiscordErrorSlug
} from '$lib/server/discord-oauth';
import {
  persistSetup,
  pinnedRolesOf,
  readDiscord,
  readGuildConfig,
  readLegacyBlob,
  saveDiscordModule,
  setupGuild,
  type DiscordCode,
  type DiscordConfig,
  type DiscordGuildTarget,
  type DiscordSetup
} from '$lib/server/discord-store';
import { alertOff, legacyConfigFor } from '@bagel/kit';
import { auditDashboardImpersonation } from '$lib/server/services';
import { redirect, isRedirect } from '@sveltejs/kit';

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  const uid = requireDiscordActor(locals);
  const target = { userId: uid, guildId: await exchangedGuild(cookies, url, uid) };
  try {
    throw redirect(302, `/discord/${target.guildId}?${await connectGuild(locals, cookies, url, target)}`);
  } catch (err) {
    if (isRedirect(err)) throw err;
    logger.error({ err }, '[discord-callback] persist failed');
    discordFail('setup');
  }
};

async function exchangedGuild(cookies: Cookies, url: URL, uid: string): Promise<string> {
  const guildId = await exchangeInstallCode(takeInstallCode(cookies, url, uid)).catch((err) => {
    logger.warn({ err }, '[discord-callback] code exchange failed');
    return '';
  });
  if (!guildId) discordFail('oauth');
  return guildId;
}

function takeInstallCode(cookies: Cookies, url: URL, uid: string): string {
  // The state cookie is sealed to this signed-in user, so a state planted in
  // the browser by somebody else cannot bind their server to this account.
  if (!discordStateOK({ cookies, url, leg: DISCORD_INSTALL_LEG, uid })) discordFail('state');
  const code = (url.searchParams.get('code') ?? '').trim();
  if (!code) discordFail('oauth');
  return code;
}

/** Redirects naming the refusal. Every dingress code is also an `?e=` slug, so
 *  the page explains it with the same word the RPC used; anything unrecognised
 *  falls back to the generic setup failure. */
function failWithCode(code: DiscordCode): never {
  const slug = (DISCORD_ERROR_SLUGS as readonly string[]).includes(code)
    ? (code as DiscordErrorSlug)
    : 'setup';
  discordFail(slug);
}

// connectGuild binds the guild, fills or adopts its layout, writes the guild's
// own config row, and returns the query string for the dashboard redirect.
// The module row is only touched to turn Discord on: a broadcaster adding a
// second server must not have their first one's settings rewritten.
async function connectGuild(
  locals: App.Locals,
  cookies: Cookies,
  url: URL,
  target: DiscordGuildTarget
): Promise<string> {
  const login = await connectLogin(locals, target);
  const seeded = await seedConfig(target, login);
  const result = await setupGuild(
    {
      ...target,
      subscribers: alertOff(seeded.subscribersEnabled),
      pinnedRoles: pinnedRolesOf(seeded),
      // The actor, not the account: a staff member installing on a
      // broadcaster's behalf is exactly what this field has to record.
      installedBy: locals.session?.user_id ?? target.userId
    },
    seeded
  );
  refuseSetup(result, cookies, url, target);
  const saved = await persistSetup(target, { ...result.config, twitchLogin: login });
  if (saved.error || saved.code) failWithCode(saved.code);
  // Only now is the blob safe to narrow: the guild row holds what it held.
  await saveDiscordModule({ userId: target.userId, enabled: true, twitchLogin: login });
  auditDashboardImpersonation(locals.session, 'discord:connect', target.guildId);
  if (result.refused) return 'connected=1&refused=1';
  return 'connected=1';
}

/** The Twitch login this install is recorded under. The board read still runs
 *  when the session carries a login, because a staff member installing on a
 *  broadcaster's behalf holds their own login, not the broadcaster's. */
async function connectLogin(locals: App.Locals, target: DiscordGuildTarget): Promise<string> {
  const view = await readDiscord({ userId: target.userId }).catch(() => null);
  return locals.session?.login ? locals.session.login : (view?.twitchLogin ?? '');
}

/**
 * The config this install starts from.
 *
 * A failed config.get is NOT "this guild has no config". Seeding a blank one
 * over a server that already had channels picked wipes every id the streamer
 * chose, and re-installing the bot is exactly when that read is most likely to
 * be slow. Refuse the whole callback instead: the binding is untouched and the
 * streamer can try again.
 *
 * A board that predates the multi-guild split still carries this guild's whole
 * config in the per-user modules blob; it has to be copied onto the guild row
 * BEFORE the blob is narrowed, or every id in it is lost.
 */
async function seedConfig(target: DiscordGuildTarget, login: string): Promise<DiscordConfig> {
  const row = await readGuildConfig(target).catch((err) => {
    logger.warn({ err }, '[discord-callback] guild config unreadable');
    discordFail('discord_unavailable');
  });
  const legacy = row.found
    ? null
    : legacyConfigFor(await readLegacyBlob({ userId: target.userId }).catch(() => null), target.guildId);
  return {
    ...row.config,
    ...(legacy ?? {}),
    guildId: target.guildId,
    twitchLogin: login
  };
}

/**
 * Turns a refused setup into the redirect that names it.
 *
 * The refusal is named by its code, not by matching outgress's English.
 * `bound_elsewhere` is also remembered in this browser so the picker can grey
 * that server out. Continuing past any other refusal used to redirect with
 * connected=1 onto a page whose config had never been written, which read as a
 * successful install that quietly did nothing.
 */
function refuseSetup(
  result: DiscordSetup,
  cookies: Cookies,
  url: URL,
  target: DiscordGuildTarget
): void {
  if (result.code === 'bound_elsewhere') {
    rememberBoundElsewhere(cookies, url, target.guildId);
    discordFail('bound_elsewhere');
  }
  if (result.error || result.code) failWithCode(result.code);
}
