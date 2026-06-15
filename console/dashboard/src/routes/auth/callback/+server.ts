import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { decodeIdToken, OAuth2RequestError } from 'arctic';
import { twitch } from '$lib/server/oauth';
import { rpc } from '@bagel/shared/server/nats';
import { COOKIE, seal } from '$lib/server/session';
import { env } from '$env/dynamic/private';

const DASHBOARD = env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard';
const SESSION_TTL = 7 * 24 * 3600;

export const GET: RequestHandler = async ({ url, cookies }) => {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const stored = cookies.get('oauth_state');
  cookies.delete('oauth_state', { path: '/' });

  // Constant-ish state check: reject missing/mismatched state before any exchange.
  if (!code || !state || !stored || state !== stored) throw redirect(302, '/login?e=state');

  try {
    const tokens = await twitch().validateAuthorizationCode(code);
    const claims = decodeIdToken(tokens.idToken()!) as {
      sub: string;
      preferred_username: string;
    };

    const userId = claims.sub;
    const login = claims.preferred_username.toLowerCase();
    const displayName = claims.preferred_username;

    await rpc(`${DASHBOARD}.upsert_user`, {
      user_id: userId,
      username: login,
      display_name: displayName
    });

    const value = seal({
      user_id: userId,
      login: login,
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
