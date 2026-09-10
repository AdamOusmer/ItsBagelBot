// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Twitch OAuth via the shared in-repo client (@bagel/shared/server/oauth),
// which replaced the deprecated arctic package. One Twitch client built from
// env. Helix user fetch lives here too so the callback route stays thin.
import { Twitch } from '@bagel/shared/server/oauth';
import { env } from '$env/dynamic/private';
import { scopeGap } from '@bagel/shared';

// Identity + the elevated bot scopes the old dashboard requested. Driven by
// DASHBOARD_BOT_SCOPES (Doppler) so the consent matches what it always asked
// for; DASHBOARD_LOGIN_SCOPES can override the whole set.
export function scopes(): string[] {
  const override = (env.DASHBOARD_LOGIN_SCOPES ?? '').split(/\s+/).filter(Boolean);
  if (override.length) return override;
  // Broadcaster grant. Mirrors the v1 broadcaster scope set (settings.py:
  // moderator:read:followers + user:read:chat + user:write:chat) plus channel:bot
  // so the bot may act in the channel. Adds channel:read:subscriptions and bits:read
  // for EventSub access, and clips:edit so !clip can create clips on the channel
  // (Create Clip runs on the broadcaster's own token, not the bot's).
  // channel:manage:broadcast covers the stream editor (!title/!game/!tags/!marker):
  // Modify Channel Information and Create Stream Marker both run on the
  // broadcaster's own token. channel:edit:commercial is !commercial / !ad.
  // Existing grants that predate either scope skip those Helix calls until
  // the broadcaster re-consents (Reconnect Twitch on settings).
  // channel:manage:redemptions covers the whole Channel Points surface: creating,
  // editing and deleting custom rewards, subscribing to redemption events, and
  // resolving redemptions (fulfill/refund): all on the broadcaster's own token.
  // channel:read:ads authorizes the channel.ad_break.begin EventSub behind the
  // ads chat alert; grants that predate it skip that (optional) subscription
  // until the broadcaster re-consents.
  const bot = 'channel:bot moderator:read:followers user:read:chat user:write:chat channel:read:subscriptions bits:read user:read:moderated_channels clips:edit channel:manage:broadcast channel:edit:commercial channel:manage:redemptions channel:read:ads'
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

// Spotify connect for song requests (the /songqueue page). The refresh token
// itself is handed to the modules service's sealed custody, never stored
// here, and the app it authorizes against is the broadcaster's own (see
// spotifyAuthorizeURL).
//
// Search rides any user token; now-playing needs user-read-currently-playing
// (user-read-playback-state is the documented fallback). Player control,
// !skip and queueing a track onto the active device, needs
// user-modify-playback-state. Without the modify grant Spotify answers 403 on
// next/queue/play, so queue control looks broken even though chat still
// replies. DASHBOARD_SPOTIFY_SCOPES overrides the whole set, mirroring
// DASHBOARD_LOGIN_SCOPES above.
export function spotifyScopes(): string[] {
  const override = (env.DASHBOARD_SPOTIFY_SCOPES ?? '').split(/\s+/).filter(Boolean);
  if (override.length) return override;
  return ['user-read-currently-playing', 'user-read-playback-state', 'user-modify-playback-state'];
}

/**
 * True when the deployment is wired for Spotify. The fleet no longer holds a
 * Spotify application (each broadcaster registers their own) so the only
 * env left is the callback URL every one of those apps must register.
 * Callers use this to render an explainer instead of a server error: an
 * unconfigured deployment is a deployment state, not a fault.
 */
export function spotifyConfigured(): boolean {
  return !!env.SPOTIFY_REDIRECT_URI;
}

// spotifyRedirectURI is that callback URL. It is not a secret, and it is the
// one Spotify value still shared by every broadcaster: they each register it
// on their own app, and Spotify matches it byte-for-byte at both the authorize
// and the exchange step. The console shows it on the setup card for exactly
// that reason.
export function spotifyRedirectURI(): string {
  const redirect = env.SPOTIFY_REDIRECT_URI;
  if (!redirect) throw new Error('SPOTIFY_REDIRECT_URI not set');
  return redirect;
}

// spotifyAuthorizeURL builds the consent redirect for ONE broadcaster's own
// Spotify application. Only the client id rides it: the authorize step is
// public by design, which is why the console can build this while the client
// secret stays sealed in the modules service and known to gossip alone.
//
// It is assembled here rather than through the shared Spotify client because
// that client is built around a confidential-client token exchange the console
// no longer performs: the callback forwards its code to gossip instead, so
// keeping a secret-less client around would only invite someone to call
// validateAuthorizationCode on it later.
export function spotifyAuthorizeURL(clientId: string, state: string): string {
  const params = new URLSearchParams({
    response_type: 'code',
    client_id: clientId,
    redirect_uri: spotifyRedirectURI(),
    state,
    scope: spotifyScopes().join(' '),
    // show_dialog forces the consent screen even when Spotify would have
    // auto-approved. Without it a reconnect that only ADDS a scope can be
    // waved through invisibly, which is indistinguishable to the broadcaster
    // from the button doing nothing, and a partially-approved grant would
    // then look identical to a full one.
    show_dialog: 'true'
  });
  return `https://accounts.spotify.com/authorize?${params.toString()}`;
}

// spotifyScopeGap is what a stored grant is missing against what the connect
// flow asks for today. An empty recorded set means a grant from before scopes
// were recorded: unknown, reported as missing everything, because assuming it
// is complete is what leaves a broadcaster hitting 403s with no explanation.
export function spotifyScopeGap(granted: readonly string[]): string[] {
  return scopeGap(spotifyScopes(), granted);
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
// routes themselves. Used on both sides of the OAuth round trip: when the
// login route stores the destination and when the callback consumes it.
export function safeNextPath(value: string | null | undefined): string | null {
  if (!value || !value.startsWith('/')) return null;
  if (value.startsWith('//') || value.startsWith('/\\')) return null;
  if (value === '/login' || value.startsWith('/login?') || value.startsWith('/auth/')) return null;
  return value;
}

