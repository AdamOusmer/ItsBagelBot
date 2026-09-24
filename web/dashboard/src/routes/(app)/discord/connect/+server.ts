// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
  putDiscordState({ cookies, url, leg: DISCORD_PICK_LEG, uid }, state);
  throw redirect(302, discordUserAuthURL(state));
};
