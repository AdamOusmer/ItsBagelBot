// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Step one of two: user authorization. This asks only for `identify guilds`,
// which adds nothing to any server, so the streamer can be shown a picker of
// the servers they actually administer before Discord asks them to install a
// bot. Step two is /discord/pick -> /discord/callback.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from '@bagel/shared/server/oauth';
import {
  DISCORD_PICK_STATE_COOKIE,
  DISCORD_STATE_TTL_SECONDS,
  discordConfigured,
  discordUserAuthURL,
  discordFail,
  requireDiscordActor
} from '$lib/server/discord-oauth';

export const GET: RequestHandler = async ({ cookies, url, locals }) => {
  requireDiscordActor(locals);
  if (!discordConfigured()) discordFail('unconfigured');

  const state = generateState();
  cookies.set(DISCORD_PICK_STATE_COOKIE, state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: DISCORD_STATE_TTL_SECONDS
  });
  throw redirect(302, discordUserAuthURL(state));
};
