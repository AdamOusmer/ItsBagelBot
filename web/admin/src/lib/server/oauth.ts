// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Twitch } from '@bagel/kit/server/oauth';
import { env } from '$env/dynamic/private';

export function scopes(): string[] {
  return ['openid', 'user:read:email'];
}

export function twitch(): Twitch {
  const id = env.TWITCH_CLIENT_ID;
  const secret = env.TWITCH_CLIENT_SECRET;
  const redirect = env.TWITCH_REDIRECT_URI;
  if (!id || !secret || !redirect) throw new Error('TWITCH_CLIENT_ID/SECRET/REDIRECT_URI not set');
  return new Twitch(id, secret, redirect);
}

// The DASHBOARD's Twitch app: the dashboard mints and consumes this grant, so the client id must match.
export function botClientId(): string {
  const id = env.DASHBOARD_TWITCH_CLIENT_ID;
  if (!id) throw new Error('DASHBOARD_TWITCH_CLIENT_ID not set');
  return id;
}

export function botScopes(): string[] {
  return [
    'openid',
    'chat:read',
    'chat:edit',
    'user:read:chat',
    'user:write:chat',
    'user:bot',
    'moderator:read:followers',
    'moderator:read:chatters',
    'moderator:manage:banned_users',
    'moderator:manage:chat_messages',
    'moderator:manage:announcements',
    'moderator:manage:shoutouts',
    // Without this scope the bot token 401s; any new scope needs a bot re-auth.
    'user:read:moderated_channels',
    'moderator:read:suspicious_users',
    'moderator:manage:automod'
  ];
}

export function botTwitch(origin: string): Twitch {
  const id = env.DASHBOARD_TWITCH_CLIENT_ID;
  const secret = env.DASHBOARD_TWITCH_CLIENT_SECRET;
  if (!id || !secret) throw new Error('DASHBOARD_TWITCH_CLIENT_ID/SECRET not set');
  const redirect = env.BOT_REDIRECT_URI ?? `${origin}/auth/bot/callback`;
  return new Twitch(id, secret, redirect);
}
