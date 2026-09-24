// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { safeReturnPath } from '@bagel/kit/return-path';
import { Twitch } from '@bagel/kit/server/oauth';
import { env } from '$env/dynamic/private';
import { scopeGap } from '@bagel/kit';

function requiredTwitchConfig(value: string | undefined): string {
  if (!value) throw new Error('TWITCH_CLIENT_ID/SECRET/REDIRECT_URI not set');
  return value;
}

export function scopes(): string[] {
  const override = (env.DASHBOARD_LOGIN_SCOPES ?? '').split(/\s+/).filter(Boolean);
  if (override.length) return override;
  const bot = 'channel:bot moderator:read:followers user:read:chat user:write:chat channel:read:subscriptions bits:read user:read:moderated_channels clips:edit channel:manage:broadcast channel:edit:commercial channel:manage:redemptions channel:read:ads'
    .split(/\s+/)
    .filter(Boolean);
  return ['openid', 'user:read:email', ...bot];
}

export function twitch(): Twitch {
  const id = requiredTwitchConfig(env.TWITCH_CLIENT_ID);
  const secret = requiredTwitchConfig(env.TWITCH_CLIENT_SECRET);
  const redirect = requiredTwitchConfig(env.TWITCH_REDIRECT_URI);
  return new Twitch(id, secret, redirect);
}

export function spotifyScopes(): string[] {
  const override = (env.DASHBOARD_SPOTIFY_SCOPES ?? '').split(/\s+/).filter(Boolean);
  if (override.length) return override;
  return ['user-read-currently-playing', 'user-read-playback-state', 'user-modify-playback-state'];
}

export function spotifyConfigured(): boolean {
  return !!env.SPOTIFY_REDIRECT_URI;
}

export function spotifyRedirectURI(): string {
  const redirect = env.SPOTIFY_REDIRECT_URI;
  if (!redirect) throw new Error('SPOTIFY_REDIRECT_URI not set');
  return redirect;
}

export function spotifyAuthorizeURL(clientId: string, state: string): string {
  const params = new URLSearchParams({
    response_type: 'code',
    client_id: clientId,
    redirect_uri: spotifyRedirectURI(),
    state,
    scope: spotifyScopes().join(' '),
    show_dialog: 'true'
  });
  return `https://accounts.spotify.com/authorize?${params.toString()}`;
}

export function spotifyScopeGap(granted: readonly string[]): string[] {
  return scopeGap(spotifyScopes(), granted);
}

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

export function safeNextPath(value: string | null | undefined): string | null {
  const path = safeReturnPath(value);
  if (!path) return null;
  const pathname = path.split(/[?#]/, 1)[0];
  if (pathname === '/login' || pathname.startsWith('/auth/')) return null;
  return path;
}
