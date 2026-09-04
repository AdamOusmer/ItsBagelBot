// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Request-level account gates, run from hooks.server.ts on EVERY request.
//
// These gates used to live in the (app) layout load, but SvelteKit never runs
// layout loads for form actions or +server.ts endpoints — so a banned or
// revoked session kept its write access until the cookie expired. Enforcing in
// the handle hook closes that: pages, actions, data requests and API endpoints
// all pass through here. Thrown redirect()s are kit-native and are encoded
// correctly for documents, __data.json and action JSON requests alike.
//
// All checks ride the same cache fabric the layout used (push-invalidated on
// the NATS bus), so per-request cost stays ~0 and an admin ban / delegation
// revoke propagates to every replica within one request.
// The demo bypass below is gated on SvelteKit's build-time `dev` constant
// FIRST, so Rollup erases the whole branch from a production build: the
// runtime DEMO env var cannot re-open these gates on a shipped image, only in
// `vite dev`. The env half is read from process.env, NOT $env/dynamic/private:
// this module is in the boot import graph (hooks.server.ts -> guard), and even
// importing the dynamic-env proxy there deadlocks server.init (exit 13).
import { dev } from '$app/environment';
import { redirect, type RequestEvent } from '@sveltejs/kit';
import { delegateAllowedPaths, pathnameAllowed } from '@bagel/shared';
import { COOKIE, seal, type Session } from '$lib/server/session';
import { accountState, delegationAccess, isBanned, type AccountState } from '$lib/server/services';
import { RpcError } from '@bagel/shared/server/nats';
import { isSessionRevoked } from '@bagel/shared/server/session-revocation';
import { assertBetaRouteOpen } from '$lib/server/module-gate';

const DEMO = dev && process.env.DEMO === '1';

// Paths that must stay reachable with a denied session: the login + OAuth flow
// (a banned user must still be able to reach the callback's own gate), logout,
// health probes, the locale switch, the pre-login delegation-accept landing and
// the public global-statistics page (it shows nobody's account state, so a
// banned or ghost session has no more reason to be bounced off it than an
// anonymous visitor does).
const PUBLIC_PREFIXES = ['/auth', '/login', '/healthz', '/readyz', '/status', '/lang', '/delegate/accept', '/stats'];

function isPublic(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + '/'));
}

function wipe(event: RequestEvent): void {
  event.cookies.delete(COOKIE, { path: '/', secure: event.url.protocol === 'https:' });
}

// assertAccountUsable runs the three gates that judge the session itself.
// They fire CONCURRENTLY: each is an independent read (ban via cache fabric,
// revocation via Valkey, account state via users RPC), and sequential awaits
// stacked three round trips on every authed request — visible as a stalled
// first paint on the dashboard. Outcomes keep their priority: ban outranks
// revocation, which outranks ghost-session; only an authoritative answer wipes
// the cookie, transport blips fail open exactly as before.
//   * ban — isBanned serves last-known state through a users-service outage.
//   * revocation — logout kills this sid; "sign out everywhere" kills every
//     session issued before that moment. isSessionRevoked never throws.
//   * ghost session — only an RpcError ("no such user") wipes; anything else
//     keeps the session and lets pages degrade.
// publishAccountState hands the gate's own account read to the (app) layout via
// locals, so the shell does not spend a second RPC on it.
//
// Settled result, never a live rejected promise (a request that never reads
// locals must not raise an unhandled rejection): a fulfilled read is reused by
// the layout, an RpcError means the user is gone (no retry), and any other
// failure leaves the field unset so the layout retries like it used to.
function publishAccountState(event: RequestEvent, state: PromiseSettledResult<AccountState>): void {
  if (state.status === 'fulfilled') event.locals.accountState = { value: state.value };
  else if (state.reason instanceof RpcError) event.locals.accountState = { ghost: true };
}

// refusalSlug reduces three gates that each answer in a different shape — a
// boolean, a boolean, a rejection kind — to the single question the caller
// actually has: which `?e=` slug, if any, refuses this session.
//
// Kept separate from the wipe/redirect so that pair is written once instead of
// three times; the order is the precedence, most conclusive first.
function refusalSlug(
  ban: PromiseSettledResult<boolean>,
  revoked: PromiseSettledResult<boolean>,
  state: PromiseSettledResult<AccountState>
): string | null {
  if (ban.status === 'fulfilled' && ban.value) return 'banned';
  if (revoked.status === 'fulfilled' && revoked.value) return 'revoked';
  if (state.status === 'rejected' && state.reason instanceof RpcError) return 'signedout';
  return null;
}

async function assertAccountUsable(event: RequestEvent, s: Session): Promise<void> {
  const [ban, revoked, state] = await Promise.allSettled([
    isBanned(s.user_id),
    isSessionRevoked({ sid: s.sid, userId: s.user_id, iat: s.iat }),
    accountState(s.user_id)
  ]);

  publishAccountState(event, state);

  const slug = refusalSlug(ban, revoked, state);
  if (!slug) return;
  wipe(event);
  throw redirect(303, `/login?e=${slug}`);
}

// The delegate's live grant on the board they are browsing, as the three
// callers below need it: the grant itself, null when the board is
// authoritatively gone (owner banned, or the share revoked), and undefined on a
// transport blip — fail open there, exactly as the account gates above do.
type Grant = { owner_user_id: string; owner_login: string; sections: string[] };

async function liveGrant(s: Session, ownerId: string): Promise<Grant | null | undefined> {
  if (await isBanned(ownerId)) return null;
  try {
    const grants = await delegationAccess(s.user_id);
    return grants.find((g) => g.owner_user_id === ownerId) ?? null;
  } catch {
    return undefined;
  }
}

function sameSections(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((sec, i) => sec === b[i]);
}

// An owner who re-scopes a live grant (dropping billing, say) used to be
// ignored until the delegate's 7-day cookie expired, because the section list
// is carried IN that cookie. Re-seal it here from the authoritative grant so
// the scope check below — and every per-page gate after it — reads the
// narrowed list on this very request. iat and expires_at ride through
// unchanged, so a re-seal never extends the session's own life.
function resealSections(event: RequestEvent, s: Session, sections: string[]): void {
  if (sameSections(s.sections ?? [], sections)) return;
  s.sections = sections;
  try {
    event.cookies.set(COOKIE, seal(s), {
      path: '/',
      httpOnly: true,
      secure: event.url.protocol === 'https:',
      sameSite: 'lax',
      maxAge: Math.max(1, s.expires_at - Math.floor(Date.now() / 1000))
    });
  } catch {
    /* seal failure: the in-memory session still carries the narrowed grant */
  }
}

// guardDelegateBoard runs the gates that only a delegated session faces: the
// board still exists, the cookie's sections still match the grant, and the
// requested (app) path is inside them.
async function guardDelegateBoard(event: RequestEvent, s: Session, ownerId: string): Promise<void> {
  // A delegate board dies out from under the session when the owner is banned
  // or revokes the share. Bounce through /delegate/exit, which re-seals the
  // visitor's own normal session; delegationAccess is push-invalidated on the
  // delegation scope, so a revoke lands within one request.
  const grant = await liveGrant(s, ownerId);
  if (grant === null) throw redirect(303, '/delegate/exit');
  if (grant) resealSections(event, s, grant.sections ?? []);

  // Section scope for everything under (app) — pages AND their actions
  // (the per-page gates remain as defense in depth).
  if (!event.route.id?.startsWith('/(app)')) return;
  const sections = s.sections ?? [];
  const allowed = delegateAllowedPaths(sections);
  if (!pathnameAllowed(event.url.pathname, allowed, sections)) {
    throw redirect(303, allowed[0] ?? '/delegate/exit');
  }
}

// guardSession validates an already-opened session against authoritative
// account state. Returns the session to keep in locals, or throws a redirect
// (wiping the cookie when the session itself is dead). Anonymous requests
// never reach this — the (app) layout owns the login redirect for pages and
// endpoints already 401 on a missing session.
export async function guardSession(event: RequestEvent, s: Session): Promise<Session> {
  if (DEMO || isPublic(event.url.pathname)) return s;

  await assertAccountUsable(event, s);

  if (s.delegate_of && event.url.pathname !== '/delegate/exit') {
    await guardDelegateBoard(event, s, s.delegate_of);
  }

  // After the delegate block so a dead board exits first; the beta gate reads
  // the delegate's board tier, not their own.
  await assertBetaRouteOpen(event);
  return s;
}
