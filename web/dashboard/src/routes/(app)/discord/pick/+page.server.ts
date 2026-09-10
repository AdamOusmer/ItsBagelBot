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
import { canManageGuild, guildMonogram, guildPickerBadge, type GuildPickerBadge } from '@bagel/kit';
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
  openURL: string;
};

export const load: PageServerLoad = async ({ locals, cookies, url }) => {
  gateModulePage(locals.session, 'discord');
  if (DEMO) return demoPick(cookies);

  // The uid comes first now: the state cookie is sealed to it, so the identity
  // has to be known before the callback's state can be checked at all.
  const uid = requireDiscordActor(locals);
  const guilds = await manageableGuilds(takePickCode(cookies, url, uid));
  if (guilds.length === 0) discordFail('noguilds');

  // One state cookie for the whole picker: whichever row the streamer clicks,
  // /discord/callback validates this value. A state per row would need a
  // cookie per row and buy nothing -- the guild is read from the token
  // response, not from the link that was clicked.
  const state = generateState();
  putDiscordState({ cookies, url, leg: DISCORD_INSTALL_LEG, uid }, state);

  const bound = await listGuilds({ userId: uid })
    .then((rows) => rows.map((g) => g.guildId))
    .catch((e) => {
      // Decorative: without it every row reads as a fresh install, which is
      // wrong but not harmful. Losing the picker over it would be.
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
    monogram: guildMonogram(g.name),
    badge,
    // A server this broadcaster already bound sends them to its settings, and
    // one that belongs to a different channel offers nothing: walking either
    // through Discord's consent screen ends where they already are, or at the
    // same refusal.
    installURL: badge === 'addable' ? discordInstallURL(state, g.id) : '',
    openURL: badge === 'mine' ? `/discord/${g.id}` : ''
  };
}

async function manageableGuilds(code: string): Promise<DiscordUserGuild[]> {
  const r = await listUserGuilds(code);
  // "No servers" and "Discord said no" are different sentences now: the first
  // tells the streamer to go fix their permissions, and saying it after a 429
  // sent them somewhere there was nothing to fix.
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
