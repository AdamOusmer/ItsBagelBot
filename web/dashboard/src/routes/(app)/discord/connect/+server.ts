// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Step one of two: user authorization. This asks only for `identify guilds`,
// which adds nothing to any server, so the streamer can be shown a picker of
// the servers they actually administer before Discord asks them to install a
// bot. Step two is /discord/pick -> /discord/callback.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from '@bagel/kit/server/oauth';
import {
  DISCORD_PICK_LEG,
  discordConfigured,
  discordUserAuthURL,
  discordFail,
  putDiscordState,
  requireDiscordActor
} from '$lib/server/discord-oauth';

export const GET: RequestHandler = async ({ cookies, url, locals }) => {
  const uid = requireDiscordActor(locals);
  if (!discordConfigured()) discordFail('unconfigured');

  const state = generateState();
  // Sealed to this signed-in user, so a state planted in the browser by
  // somebody else cannot be redeemed under this account.
  putDiscordState({ cookies, url, leg: DISCORD_PICK_LEG, uid }, state);
  throw redirect(302, discordUserAuthURL(state));
};
