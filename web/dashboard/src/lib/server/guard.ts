// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
import { dev } from '$app/environment';
import { redirect, type RequestEvent } from '@sveltejs/kit';
import { delegateAllowedPaths, pathnameAllowed } from '@bagel/kit';
import { COOKIE, seal, type Session } from '$lib/server/session';
import { accountState, delegationAccess, isBanned, type AccountState } from '$lib/server/services';
import { RpcError } from '@bagel/kit/server/nats';
import { isSessionRevoked } from '@bagel/kit/server/session-revocation';
import { assertBetaRouteOpen } from '$lib/server/module-gate';

const DEMO = dev && process.env.DEMO === '1';

const PUBLIC_PREFIXES = ['/auth', '/login', '/healthz', '/readyz', '/status', '/lang', '/delegate/accept', '/stats'];

function isPublic(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + '/'));
}

function wipe(event: RequestEvent): void {
  event.cookies.delete(COOKIE, { path: '/', secure: event.url.protocol === 'https:' });
}

// Keep '': users state_get answers a deleted account with no code; dropping it loops /goodbye.
const GONE_CODES: ReadonlySet<string> = new Set(['not_found', '']);

function accountGone(state: PromiseSettledResult<AccountState>): boolean {
  if (state.status !== 'rejected') return false;
  return state.reason instanceof RpcError && GONE_CODES.has(state.reason.code);
}

function publishAccountState(event: RequestEvent, state: PromiseSettledResult<AccountState>): void {
  if (state.status === 'fulfilled') event.locals.accountState = { value: state.value };
  else if (accountGone(state)) event.locals.accountState = { ghost: true };
}

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

// iat and expires_at pass through, so a re-seal never extends the session.
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
  }
}

async function guardDelegateBoard(event: RequestEvent, s: Session, ownerId: string): Promise<void> {
  const grant = await liveGrant(s, ownerId);
  if (grant === null) throw redirect(303, '/delegate/exit');
  if (grant) resealSections(event, s, grant.sections ?? []);

  if (!event.route.id?.startsWith('/(app)')) return;
  const sections = s.sections ?? [];
  const allowed = delegateAllowedPaths(sections);
  if (!pathnameAllowed(event.url.pathname, allowed, sections)) {
    throw redirect(303, allowed[0] ?? '/delegate/exit');
  }
}

export async function guardSession(event: RequestEvent, s: Session): Promise<Session> {
  if (DEMO || isPublic(event.url.pathname)) return s;

  await assertAccountUsable(event, s);

  if (s.delegate_of && event.url.pathname !== '/delegate/exit') {
    await guardDelegateBoard(event, s, s.delegate_of);
  }

  await assertBetaRouteOpen(event);
  return s;
}
