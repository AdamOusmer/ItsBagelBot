// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The guild id comes from the code exchange, never the caller-supplied query string.
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
  blankDiscordConfig,
  persistSetup,
  pinnedRolesOf,
  readDiscord,
  readGuildConfig,
  readLegacyBlob,
  refusalCode,
  saveDiscordModule,
  setupGuild,
  type DiscordCode,
  type DiscordConfig,
  type DiscordGuildConfig,
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
  if (!discordStateOK({ cookies, url, leg: DISCORD_INSTALL_LEG, uid })) discordFail('state');
  const code = (url.searchParams.get('code') ?? '').trim();
  if (!code) discordFail('oauth');
  return code;
}

function failWithCode(code: DiscordCode): never {
  const slug = (DISCORD_ERROR_SLUGS as readonly string[]).includes(code)
    ? (code as DiscordErrorSlug)
    : 'setup';
  discordFail(slug);
}

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
      installedBy: locals.session?.user_id ?? target.userId
    },
    seeded
  );
  refuseSetup(result, cookies, url, target);
  const saved = await persistSetup(target, { ...result.config, twitchLogin: login });
  if (saved.error || saved.code) failWithCode(saved.code);
  await saveDiscordModule({ userId: target.userId, enabled: true, twitchLogin: login });
  auditDashboardImpersonation(locals.session, 'discord:connect', target.guildId);
  if (result.refused) return 'connected=1&refused=1';
  return 'connected=1';
}

async function connectLogin(locals: App.Locals, target: DiscordGuildTarget): Promise<string> {
  const view = await readDiscord({ userId: target.userId }).catch(() => null);
  return locals.session?.login ? locals.session.login : (view?.twitchLogin ?? '');
}

/** A failed config.get refuses the callback: seeding a blank row would wipe the streamer's ids. */
async function seedConfig(target: DiscordGuildTarget, login: string): Promise<DiscordConfig> {
  const row = await readGuildConfig(target).catch((err) => unboundRow(target, err));
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

function unboundRow(target: DiscordGuildTarget, err: unknown): DiscordGuildConfig {
  if (refusalCode(err) !== 'not_bound') {
    logger.warn({ err }, '[discord-callback] guild config unreadable');
    discordFail('discord_unavailable');
  }
  return {
    config: { ...blankDiscordConfig(), guildId: target.guildId },
    version: 0,
    found: false
  };
}

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
