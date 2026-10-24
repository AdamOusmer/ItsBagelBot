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
  listUserGuilds,
  putDiscordState,
  requireDiscordActor,
  takeDiscordCode,
  type DiscordUserGuild
} from '$lib/server/discord-oauth';
import { listGuilds, type DiscordGuildSummary } from '$lib/server/discord-store';
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
  repair: boolean;
  installURL: string;
  openURL: string;
};

export const load: PageServerLoad = async ({ locals, cookies, url }) => {
  gateModulePage(locals.session, 'discord');
  if (DEMO) return demoPick(cookies);

  const uid = requireDiscordActor(locals);
  const guilds = await manageableGuilds(takeDiscordCode({ cookies, url, leg: DISCORD_PICK_LEG, uid }));
  if (guilds.length === 0) discordFail('noguilds');

  const state = generateState();
  putDiscordState({ cookies, url, leg: DISCORD_INSTALL_LEG, uid }, state);

  const rows = await listGuilds({ userId: uid })
    .catch((e) => {
      logger.warn({ err: e }, '[discord-pick] bound guilds unavailable');
      return [] as DiscordGuildSummary[];
    });
  const bound = rows.map((g) => g.guildId);
  const repair = rows.filter((g) => g.needsReauth || !g.botPresent).map((g) => g.guildId);

  const elsewhere = boundElsewhereIds(cookies);
  return { choices: guilds.map((g) => choice(g, { bound, elsewhere, repair }, state)) };
};

function choice(
  g: DiscordUserGuild,
  lists: { bound: string[]; elsewhere: string[]; repair: string[] },
  state: string
): PickChoice {
  const badge = guildPickerBadge(g.id, { bound: lists.bound, elsewhere: lists.elsewhere });
  return {
    guildId: g.id,
    name: g.name,
    iconUrl: guildIconURL(g.id, g.icon),
    badge,
    repair: badge === 'mine' && lists.repair.includes(g.id),
    installURL: badge === 'addable' || badge === 'mine' ? discordInstallURL(state, g.id) : '',
    openURL: badge === 'mine' ? `/discord/${g.id}` : ''
  };
}

async function manageableGuilds(code: string): Promise<DiscordUserGuild[]> {
  const r = await listUserGuilds(code);
  if (!r.ok) discordFail(r.code);
  return r.guilds.filter(canManageGuild);
}

async function demoPick(cookies: Cookies): Promise<{ choices: PickChoice[] }> {
  const { demoDiscordPicker, demoDiscordGuilds, demoDiscordBlocked } = await import(
    '$lib/server/demo-data'
  );
  const rows = demoDiscordGuilds();
  const bound = rows.map((g) => g.guildId);
  const repair = rows.filter((g) => g.needsReauth || !g.botPresent).map((g) => g.guildId);
  const elsewhere = [...boundElsewhereIds(cookies), ...demoDiscordBlocked()];
  return {
    choices: demoDiscordPicker()
      .filter(canManageGuild)
      .map((g) => choice({ ...g, permissions: g.permissions }, { bound, elsewhere, repair }, 'demo'))
  };
}
