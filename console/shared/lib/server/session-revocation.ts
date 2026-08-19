// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Server-side revocation for the dashboard's stateless AES-256-GCM session
// cookie. The cookie alone cannot be revoked (it is self-verifying, not a
// lookup key), so this gives the console a way to kill one session (logout)
// or every session a user holds (sign out everywhere) before the cookie's
// natural expiry — closing the "cookie copied off a shared laptop stays
// valid for up to 7 days" gap.
//
// Reads and writes deliberately use different Valkey connections:
//   * isSessionRevoked reads through valkey-store.ts's node-local client —
//     the same replica every other cached read in the console uses. A
//     revocation can therefore still pass for the replication window
//     (typically milliseconds) after it lands on the master. That is an
//     accepted tradeoff on this hot per-request guard path, not something to
//     paper over with retries or a master read here.
//   * revokeSession / revokeAllForUser write through valkey-master.ts's
//     Sentinel-pinned client, because a write that only reached a replica
//     would never make it to the master at all.
//
// Fail-open throughout: any read/write failure is swallowed and logged
// rather than blocking the request or the sign-out flow — same outage
// posture as isBanned in services.ts (last-known state wins, nobody gets
// locked out by an infrastructure blip). Only a sid, a user id, and unix
// epochs are ever written to Valkey — never a client IP or any other PII.
import { logger } from './logger';
import { getRevocation, sessionRevokedAllKey, sessionRevokedKey } from './valkey-store';
import { masterClient, warmMasterClient } from './valkey-master';
import { CircuitBreaker, withTimeout } from './resilience';

const OP_TIMEOUT_MS = 200;

// One breaker for revocation writes, isolated from every other Valkey
// consumer's breaker (rate-limit.ts, valkey-store.ts) — see valkey-master.ts.
const writeBreaker = new CircuitBreaker({ name: 'valkey-session-revocation', failureThreshold: 3, resetMs: 5_000 });

/** Pre-connect the write client at boot. Best-effort, no-op when Valkey is unconfigured. */
export function warmSessionRevocation(): void {
  warmMasterClient();
}

/**
 * Revoke one session by its sid (logout). `ttlSeconds` should be the
 * session's remaining life (expires_at - now) so the key expires on its own
 * rather than lingering forever. Never throws: a Valkey blip must not block
 * logout, it only means the old cookie stays valid a little longer than
 * intended (fail-open).
 */
export async function revokeSession(sid: string, ttlSeconds: number): Promise<void> {
  const client = masterClient();
  if (!client) return;
  try {
    await writeBreaker.run(() =>
      withTimeout(
        client.set(sessionRevokedKey(sid), '1', 'EX', Math.max(1, ttlSeconds)),
        OP_TIMEOUT_MS,
        'valkey session-revocation write'
      )
    );
  } catch (err) {
    logger.warn({ err }, '[session-revocation] revokeSession failed');
  }
}

/**
 * Revoke every session a user holds, issued before `atUnixSeconds` (sign out
 * everywhere). `ttlSeconds` bounds how long the epoch marker needs to live —
 * pass the account's normal session TTL, since no session can outlive that
 * anyway. Never throws (fail-open, see revokeSession).
 */
export async function revokeAllForUser(userId: string, atUnixSeconds: number, ttlSeconds: number): Promise<void> {
  const client = masterClient();
  if (!client) return;
  try {
    await writeBreaker.run(() =>
      withTimeout(
        client.set(sessionRevokedAllKey(userId), String(atUnixSeconds), 'EX', Math.max(1, ttlSeconds)),
        OP_TIMEOUT_MS,
        'valkey session-revocation write'
      )
    );
  } catch (err) {
    logger.warn({ err }, '[session-revocation] revokeAllForUser failed');
  }
}

export interface RevocationCheck {
  /** Absent for a session sealed before this feature shipped — see below. */
  sid?: string;
  userId: string;
  /** The session's own iat, checked against the user's revoke-all epoch. */
  iat: number;
}

// Swappable in tests so the branch logic (revoked-by-sid / revoked-by-epoch /
// fail-open) can be exercised deterministically without a live Valkey.
// Defaults to the real node-local read. Test-only; never call in app code.
type ReadFn = typeof getRevocation;
let readOverride: ReadFn | undefined;
export function setRevocationReadForTests(fn: ReadFn | undefined): void {
  readOverride = fn;
}

/**
 * True when the session should be treated as signed out: its own sid was
 * individually revoked, or it was issued (`iat`) before the user's last
 * "sign out everywhere".
 *
 * A session sealed before sids existed has no sid, so logout cannot target it
 * individually — but "sign out everywhere" still must, since those are the
 * oldest cookies in the wild and the likeliest to have been copied off a
 * shared machine. The epoch check keys on user id and iat, not on the sid, so
 * it applies to them too. Nobody is signed out by this shipping: the epoch
 * key only exists once a user asks for it.
 */
export async function isSessionRevoked(input: RevocationCheck): Promise<boolean> {
  try {
    const read = readOverride ?? getRevocation;
    const [sidHit, allAt] = await read(input.sid, input.userId);
    if (sidHit !== null) return true;
    if (allAt !== null) {
      const epoch = Number(allAt);
      if (Number.isFinite(epoch) && input.iat < epoch) return true;
    }
    return false;
  } catch (err) {
    // getRevocation already fails open internally; this catch is defense in
    // depth so a future change to the read path (or an injected test stub)
    // can never turn a Valkey hiccup into a mass sign-out.
    logger.warn({ err }, '[session-revocation] isSessionRevoked failed, failing open');
    return false;
  }
}
