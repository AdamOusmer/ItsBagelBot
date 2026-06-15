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
  const bot = (env.DASHBOARD_BOT_SCOPES ?? 'channel:bot user:read:chat user:bot')
    .split(/\s+/)
    .filter(Boolean);
  return ['user:read:email', ...bot];
}

export function twitch(): Twitch {
  const id = env.TWITCH_CLIENT_ID;
  const secret = env.TWITCH_CLIENT_SECRET;
  const redirect = env.TWITCH_REDIRECT_URI;
  if (!id || !secret || !redirect) throw new Error('TWITCH_CLIENT_ID/SECRET/REDIRECT_URI not set');
  return new Twitch(id, secret, redirect);
}

export interface TwitchUser {
  id: string;
  login: string;
  display_name: string;
}

export async function fetchUser(accessToken: string): Promise<TwitchUser> {
  const res = await fetch('https://api.twitch.tv/helix/users', {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      'Client-Id': env.TWITCH_CLIENT_ID ?? ''
    }
  });
  if (!res.ok) throw new Error(`helix users ${res.status}`);
  const body = (await res.json()) as { data: TwitchUser[] };
  const u = body.data?.[0];
  if (!u) throw new Error('no user in helix response');
  return u;
}
