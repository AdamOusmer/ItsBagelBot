import type { RequestHandler } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { decodeIdToken, OAuth2RequestError } from 'arctic';
import { twitch, safeNextPath, fetchAccountEmail } from '$lib/server/oauth';
import { rpc } from '@bagel/shared/server/nats';
import { saveGrant, isBanned, delegationConsume, userLocale, setLocale } from '$lib/server/services';
import { COOKIE, seal, SESSION_TTL_SECONDS } from '$lib/server/session';
import { isLocale, LOCALE_COOKIE } from '@bagel/shared/i18n';
import { env } from '$env/dynamic/private';

const DASHBOARD = env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard';

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

// audIssuerOk validates aud == client id and iss == Twitch, guarding against
// id_token substitution attacks.
function audIssuerOk(claims: IdTokenClaims): boolean {
  const clientId = env.TWITCH_CLIENT_ID ?? '';
  const audOk = Array.isArray(claims.aud) ? claims.aud.includes(clientId) : claims.aud === clientId;
  return audOk && claims.iss === 'https://id.twitch.tv/oauth2';
}

// isBotAccount pins the bot's Twitch user id: the bot reauthorizes through the
// admin bot flow, never this broadcaster callback. ADMIN_BOT_USER_ID is the
// same env the admin flow uses; no-op if unset.
function isBotAccount(sub: string): boolean {
  const botId = env.ADMIN_BOT_USER_ID ?? '';
  return botId !== '' && sub === botId;
}

// nonceMismatch is the replay / token-swap guard: the stored nonce must equal
// the claim. arctic's Twitch.createAuthorizationURL does not accept a nonce
// param, so the login route appended it manually and we verify it here. A
// missing stored nonce skips the check.
function nonceMismatch(claims: IdTokenClaims, storedNonce: string | undefined): boolean {
  return !!storedNonce && claims.nonce !== storedNonce;
}

// missingOpenidScope is the best-effort scope guard: when Twitch echoes the
// granted scope, openid must be present. An absent scope field is not a hard
// failure.
function missingOpenidScope(claims: IdTokenClaims): boolean {
  return !!claims.scope && !claims.scope.includes('openid');
}

// claimRejection returns the /login error slug for the first failed id_token
// guard, or null when every guard passes.
function claimRejection(claims: IdTokenClaims, storedNonce: string | undefined): string | null {
  if (!audIssuerOk(claims)) return 'state';
  if (nonceMismatch(claims, storedNonce)) return 'state';
  // A bot account landing here must not mint a streamer session (which would
  // drop it onto the user dashboard) or save a grant.
  if (isBotAccount(claims.sub)) return 'bot';
  if (missingOpenidScope(claims)) return 'scope';
  return null;
}

// verifyClaims rejects a forged or substituted id_token, throwing the matching
// login redirect on the first failed guard.
function verifyClaims(claims: IdTokenClaims, storedNonce: string | undefined): void {
  const rejected = claimRejection(claims, storedNonce);
  if (rejected) throw redirect(302, `/login?e=${rejected}`);
}

function setSessionCookie(cookies: Cookies, url: URL, session: Parameters<typeof seal>[0]): void {
  cookies.set(COOKIE, seal(session), {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: SESSION_TTL_SECONDS
  });
}

function streamerSession(id: Identity) {
  const now = Math.floor(Date.now() / 1000);
  return {
    user_id: id.userId,
    login: id.login,
    display_name: id.displayName,
    role: 'streamer' as const,
    iat: now,
    expires_at: now + SESSION_TTL_SECONDS
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

// validOAuthState is the constant-ish state check: reject a missing or
// mismatched state before any code exchange.
function validOAuthState(code: string | null, state: string | null, stored: string | undefined): code is string {
  if (!code || !state) return false;
  return !!stored && state === stored;
}

// seedLocaleCookie seeds the locale cookie from the account's saved
// preference, or — if the user explicitly set a locale on this device before
// logging in — persists that choice to the account instead of overwriting it.
// Best-effort: cookie/Accept-Language still resolve a locale on failure.
async function seedLocaleCookie(cookies: Cookies, url: URL, userId: string): Promise<void> {
  try {
    const saved = await userLocale(userId);
    const existingCookie = cookies.get(LOCALE_COOKIE);
    const deviceLocale = existingCookie && isLocale(existingCookie) ? existingCookie : null;

    if (deviceLocale && deviceLocale !== saved) {
      try {
        await setLocale(userId, deviceLocale);
      } catch (err) {
        console.error('[callback] failed to sync pre-login locale to account:', err);
      }
      return;
    }
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
    /* best-effort */
  }
}

// persistGrant stores the OAuth grant (access + refresh) after the user row
// exists — the token row references it. Grant failure stays non-fatal: the
// session is still valid (the row exists), the bot just has no channel token
// yet, and the home needs-attention strip surfaces that. The user can re-auth
// to retry.
async function persistGrant(userId: string, tokens: { accessToken(): string; refreshToken(): string }): Promise<void> {
  try {
    await saveGrant(userId, tokens.accessToken(), tokens.refreshToken());
  } catch (err: unknown) {
    console.error('[callback] grant_save failed (non-fatal):', err);
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

  if (!validOAuthState(code, state, stored)) throw redirect(302, '/login?e=state');

  try {
    await completeLogin(cookies, url, code, storedNonce);
  } catch (e) {
    if (e instanceof OAuth2RequestError) throw redirect(302, '/login?e=oauth');
    throw e;
  }

  // Owner session minted: honor the stored deep link (delegate sessions
  // redirect inside completeLogin and never reach this).
  throw redirect(302, next ?? '/');
};

// completeLogin exchanges the code, verifies the id_token, and mints the owner
// session (or hands off to the delegation accept flow, which redirects
// itself).
async function completeLogin(cookies: Cookies, url: URL, code: string, storedNonce: string | undefined): Promise<void> {
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

  // Register BEFORE the delegation accept can seal a delegate session and
  // redirect: a brand-new invitee whose first ever login is a share link still
  // needs their own user row, or the ghost-session gate reads it as a deleted
  // account, wipes the delegate session on the very next request, and bounces
  // them to /login. A returning owner just no-ops through the accept below.
  //
  // Real account email (user:read:email consent). Null on any failure —
  // capture is best-effort and the users service stores it encrypted.
  const email = await fetchAccountEmail(tokens.accessToken());
  await registerUser(identity, email);

  await acceptPendingDelegation(cookies, url, identity);

  setSessionCookie(cookies, url, streamerSession(identity));
  await seedLocaleCookie(cookies, url, identity.userId);
  await persistGrant(identity.userId, tokens);
}
