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

// Fetch the account email from Helix with the just-issued user token. The
// user:read:email scope in the login consent is what authorizes the field.
// Best-effort with a short timeout: email capture must never slow down or
// break a login, so every failure path returns null. The address is only
// forwarded to the users service (stored encrypted) and never logged here.
export async function fetchAccountEmail(accessToken: string): Promise<string | null> {
  const clientId = env.TWITCH_CLIENT_ID;
  if (!clientId) return null;
  try {
    const res = await fetch('https://api.twitch.tv/helix/users', {
      headers: { Authorization: `Bearer ${accessToken}`, 'Client-Id': clientId },
      signal: AbortSignal.timeout(2500)
    });
    if (!res.ok) return null;
    const body = (await res.json()) as { data?: Array<{ email?: string }> };
    const email = body.data?.[0]?.email?.trim() ?? '';
    return email.includes('@') ? email : null;
  } catch {
    return null;
  }
}

// Post-login deep links must stay inside the app: a single leading slash only
// (no '//' or '/\' protocol-relative escapes), and never back into the auth
// routes themselves. Used on both sides of the OAuth round trip — when the
// login route stores the destination and when the callback consumes it.
export function safeNextPath(value: string | null | undefined): string | null {
  if (!value || !value.startsWith('/')) return null;
  if (value.startsWith('//') || value.startsWith('/\\')) return null;
  if (value === '/login' || value.startsWith('/login?') || value.startsWith('/auth/')) return null;
  return value;
}

