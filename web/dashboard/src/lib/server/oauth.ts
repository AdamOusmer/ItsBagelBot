// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { safeReturnPath } from '@bagel/kit/return-path';
import { readTwitchAccount, Twitch, type TwitchAccount } from '@bagel/kit/server/oauth';
import { env } from '@bagel/kit/server/env';
import { scopeGap } from '@bagel/kit';

function requiredTwitchConfig(value: string | undefined): string {
  if (!value) throw new Error('TWITCH_CLIENT_ID/SECRET/REDIRECT_URI not set');
  return value;
}

export function scopes(): string[] {
  const override = (env.DASHBOARD_LOGIN_SCOPES ?? '').split(/\s+/).filter(Boolean);
  if (override.length) return override;
  const bot = 'moderation:read channel:moderate channel:bot moderator:read:followers user:read:chat user:write:chat channel:read:subscriptions bits:read user:read:moderated_channels clips:edit channel:manage:broadcast channel:edit:commercial channel:manage:redemptions channel:read:ads'
    .split(/\s+/)
    .filter(Boolean);
  const streamActions = [
    'channel:manage:polls',
    'channel:manage:predictions',
    'channel:manage:raids',
    'channel:manage:moderators',
    'channel:manage:vips',
    'channel:manage:ads',
    'moderator:read:unban_requests'
  ];
  return ['openid', 'user:read:email', ...bot, ...streamActions];
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

export function twitchScopeGap(granted: readonly string[] | undefined): boolean {
  return !!granted && scopeGap(scopes().filter((s) => s !== 'openid'), granted).length > 0;
}

export function fetchTwitchAccount(accessToken: string): Promise<TwitchAccount | null> {
  return readTwitchAccount(env.TWITCH_CLIENT_ID ?? '', accessToken);
}

export function safeNextPath(value: string | null | undefined): string | null {
  const path = safeReturnPath(value);
  if (!path) return null;
  const pathname = path.split(/[?#]/, 1)[0];
  if (pathname === '/login' || pathname.startsWith('/auth/')) return null;
  return path;
}
