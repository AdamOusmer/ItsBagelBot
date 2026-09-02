// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from '@bagel/shared/server/oauth';
import {
  DISCORD_STATE_COOKIE,
  DISCORD_STATE_TTL_SECONDS,
  discordConfigured,
  discordInviteURL,
  discordFail,
  requireDiscordActor
} from '$lib/server/discord-oauth';

export const GET: RequestHandler = async ({ cookies, url, locals }) => {
  requireDiscordActor(locals);
  if (!discordConfigured()) discordFail('unconfigured');

  const state = generateState();
  cookies.set(DISCORD_STATE_COOKIE, state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: DISCORD_STATE_TTL_SECONDS
  });
  throw redirect(302, discordInviteURL(state));
};
