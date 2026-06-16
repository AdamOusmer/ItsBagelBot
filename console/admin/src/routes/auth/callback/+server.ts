import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { decodeIdToken, OAuth2RequestError } from 'arctic';
import { twitch } from '$lib/server/oauth';
import { adminCheck } from '$lib/server/rpc';
import { COOKIE, seal } from '$lib/server/session';
import { env } from '$env/dynamic/private';

const SESSION_TTL = 7 * 24 * 3600;

// Twitch redirects the operator's browser here (on the tailnet). The token
// exchange below is a server-side outbound call to id.twitch.tv, so the private
// host never needs to be publicly reachable.
export const GET: RequestHandler = async ({ url, cookies }) => {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const stored = cookies.get('oauth_state');
  const storedNonce = cookies.get('oauth_nonce');
  cookies.delete('oauth_state', { path: '/' });
  cookies.delete('oauth_nonce', { path: '/' });

  if (!code || !state || !stored || state !== stored) throw redirect(302, '/login?e=state');

  try {
    const tokens = await twitch().validateAuthorizationCode(code);
    const claims = decodeIdToken(tokens.idToken()!) as {
      sub: string;
      preferred_username: string;
      aud?: string | string[];
      iss?: string;
      nonce?: string;
    };

    // aud == our client id, iss == Twitch, nonce matches (id_token swap guards).
    const clientId = env.TWITCH_CLIENT_ID ?? '';
    const audOk = Array.isArray(claims.aud)
      ? claims.aud.includes(clientId)
      : claims.aud === clientId;
    const issOk = claims.iss === 'https://id.twitch.tv/oauth2';
    if (!audOk || !issOk) throw redirect(302, '/login?e=state');
    if (storedNonce && claims.nonce !== storedNonce) throw redirect(302, '/login?e=state');

    const userId = claims.sub;
    const login = claims.preferred_username.toLowerCase();
    const displayName = claims.preferred_username;

    // Authorization gate: only sign in operators on the staff allowlist. A
    // non-staff Twitch login is rejected here and never receives a session.
    const check = await adminCheck(userId, login, displayName);
    if (!check.admin) throw redirect(302, '/login?e=denied');

    const value = seal({
      user_id: userId,
      login,
      display_name: displayName,
      role: 'streamer',
      expires_at: Math.floor(Date.now() / 1000) + SESSION_TTL
    });

    cookies.set(COOKIE, value, {
      path: '/',
      httpOnly: true,
      secure: url.protocol === 'https:',
      sameSite: 'lax',
      maxAge: SESSION_TTL
    });
  } catch (e) {
    if (e instanceof OAuth2RequestError) throw redirect(302, '/login?e=oauth');
    throw e;
  }

  throw redirect(302, '/');
};
