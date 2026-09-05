// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The server picker: the callback of the user-authorization leg, and the
// launchpad for the bot-install leg.
//
// The user access token obtained here is used once, inside listUserGuilds, to
// read /users/@me/guilds. It is never returned to this file, never persisted
// and never logged.
import type { PageServerLoad } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { logger } from '@bagel/shared/server/logger';
import { generateState } from '@bagel/shared/server/oauth';
import {
  DISCORD_PICK_STATE_COOKIE,
  DISCORD_STATE_COOKIE,
  DISCORD_STATE_TTL_SECONDS,
  discordFail,
  discordInstallURL,
  listUserGuilds,
  requireDiscordActor,
  type DiscordUserGuild
} from '$lib/server/discord-oauth';
import { listGuilds } from '$lib/server/discord-store';
import { canManageGuild, guildMonogram, guildPickerBadge, type GuildPickerBadge } from '@bagel/shared';
import { gateModulePage } from '$lib/server/module-gate';
import { dev } from '$app/environment';

// process.env, not $env/dynamic/private: this route sits behind guard.ts on
// the boot import graph (see module-gate.ts).
const DEMO = dev && process.env.DEMO === '1';

export type PickChoice = {
  guildId: string;
  name: string;
  monogram: string;
  badge: GuildPickerBadge;
  installURL: string;
};

export const load: PageServerLoad = async ({ locals, cookies, url }) => {
  gateModulePage(locals.session, 'discord');
  if (DEMO) return demoPick();

  const uid = requireDiscordActor(locals);
  const guilds = await manageableGuilds(takePickCode(cookies, url));
  if (guilds.length === 0) discordFail('noguilds');

  // One state cookie for the whole picker: whichever row the streamer clicks,
  // /discord/callback validates this value. A state per row would need a
  // cookie per row and buy nothing -- the guild is read from the token
  // response, not from the link that was clicked.
  const state = generateState();
  cookies.set(DISCORD_STATE_COOKIE, state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: DISCORD_STATE_TTL_SECONDS
  });

  const bound = await listGuilds({ userId: uid })
    .then((rows) => rows.map((g) => g.guildId))
    .catch((e) => {
      // Decorative: without it every row reads as a fresh install, which is
      // wrong but not harmful. Losing the picker over it would be.
      logger.warn({ err: e }, '[discord-pick] bound guilds unavailable');
      return [] as string[];
    });

  return { choices: guilds.map((g) => choice(g, bound, state)) };
};

function choice(g: DiscordUserGuild, bound: string[], state: string): PickChoice {
  return {
    guildId: g.id,
    name: g.name,
    monogram: guildMonogram(g.name),
    badge: guildPickerBadge(g.id, bound),
    installURL: discordInstallURL(state, g.id)
  };
}

async function manageableGuilds(code: string): Promise<DiscordUserGuild[]> {
  const all = await listUserGuilds(code).catch((err) => {
    logger.warn({ err }, '[discord-pick] guild list failed');
    return [] as DiscordUserGuild[];
  });
  return all.filter(canManageGuild);
}

function takePickCode(cookies: Cookies, url: URL): string {
  const stored = cookies.get(DISCORD_PICK_STATE_COOKIE);
  cookies.delete(DISCORD_PICK_STATE_COOKIE, { path: '/', secure: url.protocol === 'https:' });
  const state = url.searchParams.get('state');
  const code = (url.searchParams.get('code') ?? '').trim();
  if (!stored) discordFail('state');
  if (!state) discordFail('state');
  if (stored !== state) discordFail('state');
  if (!code) discordFail('oauth');
  return code;
}

async function demoPick(): Promise<{ choices: PickChoice[] }> {
  const { demoDiscordPicker, demoDiscordGuilds } = await import('$lib/server/demo-data');
  const bound = demoDiscordGuilds().map((g) => g.guildId);
  return {
    choices: demoDiscordPicker()
      .filter(canManageGuild)
      .map((g) => choice({ ...g, permissions: g.permissions }, bound, 'demo'))
  };
}
