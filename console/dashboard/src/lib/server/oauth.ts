// Twitch OAuth via arctic (the maintained, well-known OAuth2 client used with
// Lucia). One Twitch client built from env. Helix user fetch lives here too so
// the callback route stays thin.
import { Twitch } from 'arctic';
import { env } from '$env/dynamic/private';

// Identity + the elevated bot scopes the old dashboard requested. Driven by
// DASHBOARD_BOT_SCOPES (Doppler) so the consent matches what it always asked
// for; DASHBOARD_LOGIN_SCOPES can override the whole set.
export function scopes(): string[] {
  const override = (env.DASHBOARD_LOGIN_SCOPES ?? '').split(/\s+/).filter(Boolean);
  if (override.length) return override;
  // Broadcaster grant. Mirrors the v1 broadcaster scope set (settings.py:
  // moderator:read:followers + user:read:chat + user:write:chat) plus channel:bot
  // so the bot may act in the channel. Adds channel:read:subscriptions and bits:read
  // for EventSub access.
  const bot = 'channel:bot moderator:read:followers user:read:chat user:write:chat channel:read:subscriptions bits:read'
    .split(/\s+/)
    .filter(Boolean);
  return ['openid', 'user:read:email', ...bot];
}

export function twitch(): Twitch {
  const id = env.TWITCH_CLIENT_ID;
  const secret = env.TWITCH_CLIENT_SECRET;
  const redirect = env.TWITCH_REDIRECT_URI;
  if (!id || !secret || !redirect) throw new Error('TWITCH_CLIENT_ID/SECRET/REDIRECT_URI not set');
  return new Twitch(id, secret, redirect);
}

