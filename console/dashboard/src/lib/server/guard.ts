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
// DEMO is read from process.env, NOT $env/dynamic/private: this module is in
// the boot import graph (hooks.server.ts -> guard), and even importing the
// dynamic-env proxy there deadlocks server.init (exit 13). process.env carries
// the same runtime value.
import { redirect, type RequestEvent } from '@sveltejs/kit';
import { MODULE_CATALOG, moduleDelegateSections } from '@bagel/shared';
import { COOKIE, type Session } from '$lib/server/session';
import { accountState, delegationAccess, isBanned } from '$lib/server/services';
import { RpcError } from '@bagel/shared/server/nats';

// Paths that must stay reachable with a denied session: the login + OAuth flow
// (a banned user must still be able to reach the callback's own gate), logout,
// health probes, the locale switch and the pre-login delegation-accept landing.
const PUBLIC_PREFIXES = ['/auth', '/login', '/healthz', '/readyz', '/lang', '/delegate/accept'];

function isPublic(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + '/'));
}

function wipe(event: RequestEvent): void {
  event.cookies.delete(COOKIE, { path: '/', secure: event.url.protocol === 'https:' });
}

// delegateAllowedPaths lists the (app) path prefixes a delegate may open: each
// granted section's own page, plus every bespoke module page whose catalog def
// is opened by one of those grants (moduleDelegateSections — the same source
// the per-page gates and the tile grid read, so the three can never drift and
// a new module page needs no edit here). Counters is loyalty's companion page
// without a tile of its own, so it rides the modules grant explicitly.
function delegateAllowedPaths(sections: readonly string[]): string[] {
  const allowed = sections.map((sec) => `/${sec}`);
  for (const def of MODULE_CATALOG) {
    if (def.href && moduleDelegateSections(def).some((sec) => sections.includes(sec))) {
      allowed.push(def.href);
    }
  }
  if (sections.includes('modules')) allowed.push('/counters');
  return allowed;
}

// guardSession validates an already-opened session against authoritative
// account state. Returns the session to keep in locals, or throws a redirect
// (wiping the cookie when the session itself is dead). Anonymous requests
// never reach this — the (app) layout owns the login redirect for pages and
// endpoints already 401 on a missing session.
export async function guardSession(event: RequestEvent, s: Session): Promise<Session> {
  if (process.env.DEMO === '1' || isPublic(event.url.pathname)) return s;

  // Platform ban — own account. Same outage posture as before: isBanned serves
  // last-known state through a users-service outage and fails open only with
  // no cached state at all.
  if (await isBanned(s.user_id)) {
    wipe(event);
    throw redirect(303, '/login?e=banned');
  }

  // Ghost-session gate: only an authoritative "no such user" (RpcError) wipes
  // the cookie; a transport blip keeps the session and pages degrade instead.
  try {
    await accountState(s.user_id);
  } catch (err) {
    if (err instanceof RpcError) {
      wipe(event);
      throw redirect(303, '/login?e=signedout');
    }
  }

  if (s.delegate_of && event.url.pathname !== '/delegate/exit') {
    // A delegate board dies out from under the session when the owner is
    // banned or revokes the share. Bounce through /delegate/exit, which
    // re-seals the visitor's own normal session; delegationAccess is
    // push-invalidated on the delegation scope, so a revoke lands within one
    // request. Fail open on transport blips, matching the gates above.
    let boardGone = await isBanned(s.delegate_of);
    if (!boardGone) {
      try {
        const grants = await delegationAccess(s.user_id);
        boardGone = !grants.some((g) => g.owner_user_id === s.delegate_of);
      } catch {
        /* transport blip: keep the session */
      }
    }
    if (boardGone) throw redirect(303, '/delegate/exit');

    // Section scope for everything under (app) — pages AND their actions
    // (the per-page gates remain as defense in depth).
    if (event.route.id?.startsWith('/(app)')) {
      const allowed = delegateAllowedPaths(s.sections ?? []);
      const ok = allowed.some((p) => event.url.pathname === p || event.url.pathname.startsWith(p + '/'));
      if (!ok) throw redirect(303, allowed[0] ?? '/delegate/exit');
    }
  }

  return s;
}
