import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { decodeIdToken, OAuth2RequestError } from 'arctic';
import { twitch } from '$lib/server/oauth';
import { rpc } from '@bagel/shared/server/nats';
import { saveGrant, isBanned, delegationConsume } from '$lib/server/rpc';
import { COOKIE, seal } from '$lib/server/session';
import { env } from '$env/dynamic/private';

const DASHBOARD = env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard';
const SESSION_TTL = 7 * 24 * 3600;

export const GET: RequestHandler = async ({ url, cookies }) => {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const stored = cookies.get('oauth_state');
  const storedNonce = cookies.get('oauth_nonce');
  cookies.delete('oauth_state', { path: '/' });
  cookies.delete('oauth_nonce', { path: '/' });

  // Constant-ish state check: reject missing/mismatched state before any exchange.
  if (!code || !state || !stored || state !== stored) throw redirect(302, '/login?e=state');

  try {
    const tokens = await twitch().validateAuthorizationCode(code);
    const claims = decodeIdToken(tokens.idToken()!) as {
      sub: string;
      preferred_username: string;
      aud?: string | string[];
      iss?: string;
      nonce?: string;
      // Twitch may include granted scope; best-effort check.
      scope?: string;
    };

    // Validate aud == client id and iss == Twitch.
    // arctic's Twitch.createAuthorizationURL does not accept a nonce param,
    // so we appended it manually in the login route and verify it here.
    // This guards against id_token substitution attacks.
    const clientId = env.TWITCH_CLIENT_ID ?? '';
    const audOk = Array.isArray(claims.aud)
      ? claims.aud.includes(clientId)
      : claims.aud === clientId;
    const issOk = claims.iss === 'https://id.twitch.tv/oauth2';
    if (!audOk || !issOk) throw redirect(302, '/login?e=state');

    // Nonce check: stored nonce must match claim (replay / token-swap guard).
    if (storedNonce && claims.nonce !== storedNonce) throw redirect(302, '/login?e=state');

    // Bot-account guard: the bot reauthorizes through the admin bot flow, not
    // here. If the bot account ever lands on this broadcaster callback, refuse to
    // mint a streamer session (which would drop it onto the user dashboard) and
    // skip the grant save. claims.sub is the Twitch user id; ADMIN_BOT_USER_ID is
    // the same env the admin flow uses to pin the bot's id. No-op if unset.
    const botId = env.ADMIN_BOT_USER_ID ?? '';
    if (botId && claims.sub === botId) throw redirect(302, '/login?e=bot');

    // Best-effort scope check: if Twitch echoes the granted scope, ensure
    // openid is present. Don't hard-fail on absent scope field.
    if (claims.scope && !claims.scope.includes('openid')) throw redirect(302, '/login?e=scope');

    const userId = claims.sub;
    const login = claims.preferred_username.toLowerCase();
    const displayName = claims.preferred_username;

    // Platform ban gate: a banned user must not get a session. isBanned fails
    // open (treats an RPC blip as not-banned) so an outage never locks out
    // every login; the admin panel re-bans authoritatively.
    if (await isBanned(userId)) throw redirect(302, '/login?e=banned');

    // Delegated accept flow: if a pending share token rode in on a cookie, bind
    // it to this user now. Single-use — consume always deletes the cookie, and a
    // delegate session is sealed only on success. On any failure we fall through
    // to /login?e=link instead of issuing a normal owner session.
    const pending = cookies.get('pending_delegation');
    if (pending) {
      cookies.delete('pending_delegation', { path: '/' });
      const result = await delegationConsume(pending, userId, login);
      if (!result.ok) throw redirect(302, '/login?e=link');

      const value = seal({
        user_id: userId,
        login,
        display_name: displayName,
        role: 'streamer',
        expires_at: Math.floor(Date.now() / 1000) + SESSION_TTL,
        delegate_of: result.owner_user_id,
        delegate_login: result.owner_login,
        sections: result.sections ?? []
      });
      cookies.set(COOKIE, value, {
        path: '/',
        httpOnly: true,
        secure: url.protocol === 'https:',
        sameSite: 'lax',
        maxAge: SESSION_TTL
      });
      throw redirect(302, '/');
    }

    // Issue session cookie immediately — no NATS round-trip on the hot path.
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

    // Register the user, then persist the OAuth grant (access + refresh). The
    // token row references the user, so upsert must land first. Both are awaited:
    // skipping grant_save is exactly the bug where login succeeds but the bot
    // never gets a token to act in the channel. A failure here is logged but does
    // not block sign-in — the user can re-auth to retry.
    try {
      await rpc(`${DASHBOARD}.upsert_user`, {
        user_id: userId,
        username: login,
        display_name: displayName
      });
      await saveGrant(userId, tokens.accessToken(), tokens.refreshToken());
    } catch (err: unknown) {
      console.error('[callback] upsert_user/grant_save failed (non-fatal):', err);
    }
  } catch (e) {
    if (e instanceof OAuth2RequestError) throw redirect(302, '/login?e=oauth');
    throw e;
  }

  throw redirect(302, '/');
};
