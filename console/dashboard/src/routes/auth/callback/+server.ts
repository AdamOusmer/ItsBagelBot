import type { RequestHandler } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { decodeIdToken, OAuth2RequestError } from 'arctic';
import { twitch, safeNextPath, fetchAccountEmail } from '$lib/server/oauth';
import { rpc } from '@bagel/shared/server/nats';
import { saveGrant, isBanned, delegationConsume } from '$lib/server/services';
import { COOKIE, seal } from '$lib/server/session';
import { env } from '$env/dynamic/private';

const DASHBOARD = env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard';
const SESSION_TTL = 7 * 24 * 3600;

type IdTokenClaims = {
  sub: string;
  preferred_username: string;
  aud?: string | string[];
  iss?: string;
  nonce?: string;
  // Twitch may include granted scope; best-effort check.
  scope?: string;
};

type Identity = { userId: string; login: string; displayName: string };

// verifyClaims rejects a forged or substituted id_token. Throws a redirect on
// any failed guard.
function verifyClaims(claims: IdTokenClaims, storedNonce: string | undefined): void {
  // Validate aud == client id and iss == Twitch.
  // arctic's Twitch.createAuthorizationURL does not accept a nonce param,
  // so we appended it manually in the login route and verify it here.
  // This guards against id_token substitution attacks.
  const clientId = env.TWITCH_CLIENT_ID ?? '';
  const audOk = Array.isArray(claims.aud) ? claims.aud.includes(clientId) : claims.aud === clientId;
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
}

function setSessionCookie(cookies: Cookies, url: URL, session: Parameters<typeof seal>[0]): void {
  cookies.set(COOKIE, seal(session), {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: SESSION_TTL
  });
}

function streamerSession(id: Identity) {
  return {
    user_id: id.userId,
    login: id.login,
    display_name: id.displayName,
    role: 'streamer' as const,
    expires_at: Math.floor(Date.now() / 1000) + SESSION_TTL
  };
}

// Delegated accept flow: if a pending share token rode in on a cookie, bind
// it to this user now. Single-use — consume always deletes the cookie, and a
// delegate session is sealed only on success. On any failure we redirect to
// /login?e=link instead of issuing a normal owner session. No-op without the
// cookie; when it consumes, it throws the final redirect itself.
async function acceptPendingDelegation(cookies: Cookies, url: URL, id: Identity): Promise<void> {
  const pending = cookies.get('pending_delegation');
  if (!pending) return;

  cookies.delete('pending_delegation', { path: '/' });
  const result = await delegationConsume(pending, id.userId, id.login);
  if (!result.ok) throw redirect(302, '/login?e=link');

  setSessionCookie(cookies, url, {
    ...streamerSession(id),
    delegate_of: result.owner_user_id,
    delegate_login: result.owner_login,
    sections: result.sections ?? []
  });
  throw redirect(302, '/');
}

// Register the user BEFORE sealing a session. The (app) layout's
// ghost-session gate treats a missing user row as a deleted account and
// wipes the cookie, so minting a session for a row that failed to land
// is an instant sign-out loop. If the upsert cannot land, refuse the
// session and let the user retry the flow.
async function registerUser(id: Identity, email: string | null): Promise<void> {
  try {
    await rpc(`${DASHBOARD}.upsert_user`, {
      user_id: id.userId,
      username: id.login,
      display_name: id.displayName,
      ...(email ? { email } : {})
    });
  } catch (err: unknown) {
    console.error('[callback] upsert_user failed, refusing session:', err);
    throw redirect(302, '/login?e=retry');
  }
}

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
    const claims = decodeIdToken(tokens.idToken()!) as IdTokenClaims;
    verifyClaims(claims, storedNonce);

    const identity: Identity = {
      userId: claims.sub,
      login: claims.preferred_username.toLowerCase(),
      displayName: claims.preferred_username
    };

    // Platform ban gate: a banned user must not get a session. isBanned fails
    // open (treats an RPC blip as not-banned) so an outage never locks out
    // every login; the admin panel re-bans authoritatively.
    if (await isBanned(identity.userId)) throw redirect(302, '/login?e=banned');

    await acceptPendingDelegation(cookies, url, identity);

    // Real account email (user:read:email consent). Null on any failure —
    // capture is best-effort and the users service stores it encrypted.
    const email = await fetchAccountEmail(tokens.accessToken());
    await registerUser(identity, email);

    setSessionCookie(cookies, url, streamerSession(identity));

    // Persist the OAuth grant (access + refresh) after the user row exists —
    // the token row references it. Grant failure stays non-fatal: the session
    // is still valid (the row exists), the bot just has no channel token yet,
    // and the home needs-attention strip surfaces that. The user can re-auth
    // to retry.
    try {
      await saveGrant(identity.userId, tokens.accessToken(), tokens.refreshToken());
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
