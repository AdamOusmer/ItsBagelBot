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
