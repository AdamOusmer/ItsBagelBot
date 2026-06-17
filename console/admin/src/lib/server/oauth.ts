// Twitch OAuth via arctic, identity-only. The admin console authenticates an
// operator's Twitch account to obtain their subject id; authorization is then
// decided by the DB allowlist (auth.check). It requests no bot scopes: sign-in
// proves who you are, nothing more. Reuses the same Twitch app as the dashboard
// tier (same client id/secret); only the redirect URI differs.
import { Twitch } from 'arctic';
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

// Bot authorization uses the DASHBOARD's Twitch app, not the admin one: the bot
// grant is minted and consumed by the dashboard tier, so the token must be
// issued by the same client id/secret (and that app must register the bot
// redirect URI). DASHBOARD_TWITCH_CLIENT_ID/SECRET are copied into the admin
// config (Doppler). Scopes override via BOT_OAUTH_SCOPES; redirect via
// BOT_REDIRECT_URI, else derived from the request origin.
export function botClientId(): string {
  const id = env.DASHBOARD_TWITCH_CLIENT_ID;
  if (!id) throw new Error('DASHBOARD_TWITCH_CLIENT_ID not set');
  return id;
}

export function botScopes(): string[] {
  const override = (env.BOT_OAUTH_SCOPES ?? '').split(/\s+/).filter(Boolean);
  if (override.length) return override;
  return ['openid', 'user:read:chat', 'user:write:chat', 'user:bot', 'channel:bot'];
}

export function botTwitch(origin: string): Twitch {
  const id = env.DASHBOARD_TWITCH_CLIENT_ID;
  const secret = env.DASHBOARD_TWITCH_CLIENT_SECRET;
  if (!id || !secret) throw new Error('DASHBOARD_TWITCH_CLIENT_ID/SECRET not set');
  const redirect = env.BOT_REDIRECT_URI ?? `${origin}/auth/bot/callback`;
  return new Twitch(id, secret, redirect);
}
