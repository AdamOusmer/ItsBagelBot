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
import { MODULE_CATALOG, moduleDelegateSections } from '@bagel/shared';
import { COOKIE, type Session } from '$lib/server/session';
import { accountState, delegationAccess, isBanned } from '$lib/server/services';
import { RpcError } from '@bagel/shared/server/nats';
import { isSessionRevoked } from '@bagel/shared/server/session-revocation';

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

// delegateAllowedPaths lists the (app) path prefixes a delegate may open: each
// granted section's own page, plus every bespoke module page whose catalog def
// is opened by one of those grants (moduleDelegateSections — the same source
// the per-page gates and the tile grid read, so the three can never drift and
// a new module page needs no edit here). The read-only counter name list also
// opens to the commands grant so commands-only delegates can use the picker.
function delegateAllowedPaths(sections: readonly string[]): string[] {
  const allowed = sections.map((sec) => `/${sec}`);
  for (const def of MODULE_CATALOG) {
    if (def.href && moduleDelegateSections(def).some((sec) => sections.includes(sec))) {
      allowed.push(def.href);
    }
  }
  if (sections.includes('commands')) allowed.push('/counters/list');
  return allowed;
}

// pathnameAllowed checks a request path against the delegate's allowed-path
// list. An exact hit always passes; a prefix hit (a sub-route under a
// granted section) usually does too, EXCEPT under '/modules': that prefix
// covers the generic per-module reply page for every catalog module, but a
// module can declare its own narrower delegateSections (channel points), so
// admitting '/modules/<id>' on the strength of the bare 'modules' grant
// would let it reach a module it was never granted. Recheck the specific
// module's own scope in that one case; every other prefix (the counters
// list, a bespoke href) carries no per-item scope of its own.
function pathnameAllowed(pathname: string, allowed: string[], sections: readonly string[]): boolean {
  if (allowed.includes(pathname)) return true;
  const prefix = allowed.find((p) => pathname.startsWith(p + '/'));
  if (!prefix) return false;
  if (prefix !== '/modules') return true;
  const id = pathname.slice(prefix.length + 1).split('/')[0];
  return moduleSubpathAllowed(id, sections);
}

function moduleSubpathAllowed(id: string, sections: readonly string[]): boolean {
  const def = MODULE_CATALOG.find((d) => d.id === id);
  // An id the catalog has never heard of 404s at the route itself; let the
  // request through rather than duplicating that check here.
  if (!def) return true;
  return moduleDelegateSections(def).some((sec) => sections.includes(sec));
}

// guardSession validates an already-opened session against authoritative
// account state. Returns the session to keep in locals, or throws a redirect
// (wiping the cookie when the session itself is dead). Anonymous requests
// never reach this — the (app) layout owns the login redirect for pages and
// endpoints already 401 on a missing session.
export async function guardSession(event: RequestEvent, s: Session): Promise<Session> {
  if (DEMO || isPublic(event.url.pathname)) return s;

  // Platform ban — own account. Same outage posture as before: isBanned serves
  // last-known state through a users-service outage and fails open only with
  // no cached state at all.
  if (await isBanned(s.user_id)) {
    wipe(event);
    throw redirect(303, '/login?e=banned');
  }

  // Server-side revocation: logout kills just this sid, "sign out
  // everywhere" (settings) kills every sid issued before that moment. Same
  // fail-open posture as the ban check above — isSessionRevoked never throws.
  // Sessions minted before this feature shipped carry no sid and are treated
  // as not revocable (see session-revocation.ts), so this never mass-signs-out
  // everyone already logged in on deploy.
  if (await isSessionRevoked({ sid: s.sid, userId: s.user_id, iat: s.iat })) {
    wipe(event);
    throw redirect(303, '/login?e=signedout');
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
      const sections = s.sections ?? [];
      const allowed = delegateAllowedPaths(sections);
      if (!pathnameAllowed(event.url.pathname, allowed, sections)) {
        throw redirect(303, allowed[0] ?? '/delegate/exit');
      }
    }
  }

  return s;
}
