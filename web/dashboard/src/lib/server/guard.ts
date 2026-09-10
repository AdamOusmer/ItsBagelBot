// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Request-level account gates, run from hooks.server.ts on EVERY request.
//
// These gates used to live in the (app) layout load, but SvelteKit never runs
// layout loads for form actions or +server.ts endpoints, so a banned or
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
import { delegateAllowedPaths, pathnameAllowed } from '@bagel/kit';
import { COOKIE, seal, type Session } from '$lib/server/session';
import { accountState, delegationAccess, isBanned, type AccountState } from '$lib/server/services';
import { RpcError } from '@bagel/kit/server/nats';
import { isSessionRevoked } from '@bagel/kit/server/session-revocation';
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
// stacked three round trips on every authed request, visible as a stalled
// first paint on the dashboard. Outcomes keep their priority: ban outranks
// revocation, which outranks ghost-session; only an authoritative answer wipes
// the cookie, transport blips fail open exactly as before.
//   * ban: isBanned serves last-known state through a users-service outage.
//   * revocation: logout kills this sid; "sign out everywhere" kills every
//     session issued before that moment. isSessionRevoked never throws.
//   * ghost session: only an authoritative "no such user" wipes (accountGone
//     below); a service-side refusal or a transport blip keeps the session and
//     lets pages degrade.
// publishAccountState hands the gate's own account read to the (app) layout via
// locals, so the shell does not spend a second RPC on it.
//
// The refusal codes that mean "this session's user is gone", as opposed to
// "the users service could not answer right now".
//
// Every RpcError used to land here as gone, so a single `internal` or
// `unavailable` refusal from the users projection signed a live visitor out
// mid-session. Only `not_found` is authoritative about the row being absent;
// the other codes are the service talking about itself, and a session must
// survive them.
//
// The empty code stays in this set ON PURPOSE, and is not an oversight:
// app/db/users/rpc/dashboard.go answers state_get through
// `respondErr(msg, err.Error())`, which writes no `code` field at all, so the
// deleted-account reply reaches this gate uncoded today -- not only from some
// hypothetical older build. A strict `code === 'not_found'` test would
// therefore stop clearing ghost sessions outright and re-open the Jul 2 2026
// /goodbye bounce, where a deleted-then-recreated account held a valid cookie
// for a nonexistent row and looped through the sign-out gate. The trade is
// asymmetric: clearing wrongly costs one re-login, not clearing costs that
// loop, so an uncoded refusal keeps the pre-code behaviour exactly.
// Rejected alternative: teaching rpc-code.ts's LEGACY_TEXT a "no such user"
// substring. That table only fires for codes the caller already knows, is
// dated for deletion after 2026-10-08, and hanging the ghost gate off a
// sentence match is the precise bug class the code vocabulary removed.
// Revisit: once the users dashboard handlers answer with codes, drop '' here
// and this set becomes the single `not_found`.
const GONE_CODES: ReadonlySet<string> = new Set(['not_found', '']);

// accountGone is the one reading of a settled account read that both the
// locals publication and the refusal slug below share. One predicate rather
// than the same test written twice: the two must never disagree about whether
// a code means gone, or a request would wipe the cookie while telling the
// layout to retry.
function accountGone(state: PromiseSettledResult<AccountState>): boolean {
  if (state.status !== 'rejected') return false;
  return state.reason instanceof RpcError && GONE_CODES.has(state.reason.code);
}

// Settled result, never a live rejected promise (a request that never reads
// locals must not raise an unhandled rejection): a fulfilled read is reused by
// the layout, a gone verdict means no retry, and any other failure -- a
// non-RpcError blip, or an `internal`/`unavailable` refusal -- leaves the
// field unset so the layout retries like it used to.
function publishAccountState(event: RequestEvent, state: PromiseSettledResult<AccountState>): void {
  if (state.status === 'fulfilled') event.locals.accountState = { value: state.value };
  else if (accountGone(state)) event.locals.accountState = { ghost: true };
}

// refusalSlug reduces three gates that each answer in a different shape (a
// boolean, a boolean, a rejection kind) to the single question the caller
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
  if (accountGone(state)) return 'signedout';
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
// transport blip, fail open there, exactly as the account gates above do.
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
// the scope check below (and every per-page gate after it) reads the
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

  // Section scope for everything under (app): pages AND their actions
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
// never reach this: the (app) layout owns the login redirect for pages and
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
