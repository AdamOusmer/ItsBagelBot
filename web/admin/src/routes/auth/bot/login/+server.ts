// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { randomBytes } from 'node:crypto';
import { generateState } from '@bagel/kit/server/oauth';
import { botTwitch, botScopes } from '$lib/server/oauth';
import { env } from '$env/dynamic/private';

export const GET: RequestHandler = ({ cookies, url }) => {
  if (!env.TWITCH_BOT_USER_ID?.trim()) {
    throw redirect(302, '/auth/bot/done?e=config');
  }

  const state = generateState();
  const nonce = randomBytes(16).toString('base64url');
  const authUrl = botTwitch(url.origin).createAuthorizationURL(state, botScopes());
  authUrl.searchParams.set('force_verify', 'true');
  authUrl.searchParams.set('nonce', nonce);

  const cookieOptions = {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax' as const,
    maxAge: 600
  };
  cookies.set('bot_oauth_state', state, cookieOptions);
  cookies.set('bot_oauth_nonce', nonce, cookieOptions);

  throw redirect(302, authUrl.toString());
};
