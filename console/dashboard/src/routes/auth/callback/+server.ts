import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { decodeIdToken, OAuth2RequestError } from 'arctic';
import { twitch, safeNextPath, fetchAccountEmail } from '$lib/server/oauth';
import { rpc } from '@bagel/shared/server/nats';
import { saveGrant, isBanned, delegationConsume, userLocale } from '$lib/server/services';
import { COOKIE, seal } from '$lib/server/session';
import { isLocale, LOCALE_COOKIE } from '@bagel/shared/i18n';
import { env } from '$env/dynamic/private';

const DASHBOARD = env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard';
const SESSION_TTL = 7 * 24 * 3600;

export const GET: RequestHandler = async ({ url, cookies }) => {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const stored = cookies.get('oauth_state');
  const storedNonce = cookies.get('oauth_nonce');
  // Deep link stored by /auth/login (e.g. /billing?subscribe=1 from the
  // pricing page). Re-validated here: the cookie is client-writable.
  const next = safeNextPath(cookies.get('login_next'));
  cookies.delete('oauth_state', { path: '/' });
  cookies.delete('oauth_nonce', { path: '/' });
  cookies.delete('login_next', { path: '/' });

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

    // Register the user BEFORE sealing a session. The (app) layout's
    // ghost-session gate treats a missing user row as a deleted account and
    // wipes the cookie, so minting a session for a row that failed to land
    // is an instant sign-out loop. If the upsert cannot land, refuse the
    // session and let the user retry the flow.
    // Real account email (user:read:email consent). Null on any failure —
    // capture is best-effort and the users service stores it encrypted.
    const email = await fetchAccountEmail(tokens.accessToken());

    let registered = false;
    try {
      await rpc(`${DASHBOARD}.upsert_user`, {
        user_id: userId,
        username: login,
        display_name: displayName,
        ...(email ? { email } : {})
      });
      registered = true;
    } catch (err: unknown) {
      console.error('[callback] upsert_user failed, refusing session:', err);
    }
    if (!registered) throw redirect(302, '/login?e=retry');

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

    // Seed the locale cookie from the account's saved preference so the chosen
    // language follows the user to a new browser/device. Best-effort: a new user
    // defaults to 'en', and an RPC blip just leaves detection to Accept-Language.
    try {
      const saved = await userLocale(userId);
      if (isLocale(saved)) {
        cookies.set(LOCALE_COOKIE, saved, {
          path: '/',
          httpOnly: true,
          secure: url.protocol === 'https:',
          sameSite: 'lax',
          maxAge: 60 * 60 * 24 * 365
        });
      }
    } catch {
      /* best-effort — cookie/Accept-Language still resolve a locale */
    }

    // Persist the OAuth grant (access + refresh) after the user row exists —
    // the token row references it. Grant failure stays non-fatal: the session
    // is still valid (the row exists), the bot just has no channel token yet,
    // and the home needs-attention strip surfaces that. The user can re-auth
    // to retry.
    try {
      await saveGrant(userId, tokens.accessToken(), tokens.refreshToken());
    } catch (err: unknown) {
      console.error('[callback] grant_save failed (non-fatal):', err);
    }
  } catch (e) {
    if (e instanceof OAuth2RequestError) throw redirect(302, '/login?e=oauth');
    throw e;
  }

  // Owner session minted: honor the stored deep link (delegate sessions
  // redirect inside the try block and never reach this).
  throw redirect(302, next ?? '/');
};
