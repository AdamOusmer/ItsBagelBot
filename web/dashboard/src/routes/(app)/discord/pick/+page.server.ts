// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { logger } from '@bagel/kit/server/logger';
import { generateState } from '@bagel/kit/server/oauth';
import {
  DISCORD_INSTALL_LEG,
  DISCORD_PICK_LEG,
  boundElsewhereIds,
  discordFail,
  discordInstallURL,
  discordStateOK,
  listUserGuilds,
  putDiscordState,
  requireDiscordActor,
  type DiscordUserGuild
} from '$lib/server/discord-oauth';
import { listGuilds } from '$lib/server/discord-store';
import { canManageGuild, guildIconURL, guildPickerBadge, type GuildPickerBadge } from '@bagel/kit';
import { gateModulePage } from '$lib/server/module-gate';
import { dev } from '$app/environment';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

export type PickChoice = {
  guildId: string;
  name: string;
  iconUrl: string;
  badge: GuildPickerBadge;
  installURL: string;
  openURL: string;
};

export const load: PageServerLoad = async ({ locals, cookies, url }) => {
  gateModulePage(locals.session, 'discord');
  if (DEMO) return demoPick(cookies);

  const uid = requireDiscordActor(locals);
  const guilds = await manageableGuilds(takePickCode(cookies, url, uid));
  if (guilds.length === 0) discordFail('noguilds');

  const state = generateState();
  putDiscordState({ cookies, url, leg: DISCORD_INSTALL_LEG, uid }, state);

  const bound = await listGuilds({ userId: uid })
    .then((rows) => rows.map((g) => g.guildId))
    .catch((e) => {
      logger.warn({ err: e }, '[discord-pick] bound guilds unavailable');
      return [] as string[];
    });

  const elsewhere = boundElsewhereIds(cookies);
  return { choices: guilds.map((g) => choice(g, bound, elsewhere, state)) };
};

function choice(
  g: DiscordUserGuild,
  bound: string[],
  elsewhere: string[],
  state: string
): PickChoice {
  const badge = guildPickerBadge(g.id, { bound, elsewhere });
  return {
    guildId: g.id,
    name: g.name,
    iconUrl: guildIconURL(g.id, g.icon),
    badge,
    installURL: badge === 'addable' ? discordInstallURL(state, g.id) : '',
    openURL: badge === 'mine' ? `/discord/${g.id}` : ''
  };
}

async function manageableGuilds(code: string): Promise<DiscordUserGuild[]> {
  const r = await listUserGuilds(code);
  if (!r.ok) discordFail(r.code);
  return r.guilds.filter(canManageGuild);
}

function takePickCode(cookies: Cookies, url: URL, uid: string): string {
  if (!discordStateOK({ cookies, url, leg: DISCORD_PICK_LEG, uid })) discordFail('state');
  const code = (url.searchParams.get('code') ?? '').trim();
  if (!code) discordFail('oauth');
  return code;
}

async function demoPick(cookies: Cookies): Promise<{ choices: PickChoice[] }> {
  const { demoDiscordPicker, demoDiscordGuilds, demoDiscordBlocked } = await import(
    '$lib/server/demo-data'
  );
  const bound = demoDiscordGuilds().map((g) => g.guildId);
  const elsewhere = [...boundElsewhereIds(cookies), ...demoDiscordBlocked()];
  return {
    choices: demoDiscordPicker()
      .filter(canManageGuild)
      .map((g) => choice({ ...g, permissions: g.permissions }, bound, elsewhere, 'demo'))
  };
}
