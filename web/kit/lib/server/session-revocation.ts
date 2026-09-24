// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { logger } from './logger';
import { getRevocation, sessionRevokedAllKey, sessionRevokedKey } from './valkey-store';
import { masterClient, warmMasterClient } from './valkey-master';
import { CircuitBreaker, withTimeout } from './resilience';

const OP_TIMEOUT_MS = 200;

const writeBreaker = new CircuitBreaker({ name: 'valkey-session-revocation', failureThreshold: 3, resetMs: 5_000 });

export function warmSessionRevocation(): void {
  warmMasterClient();
}

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
  sid?: string;
  userId: string;
  iat: number;
}

type ReadFn = typeof getRevocation;
let readOverride: ReadFn | undefined;
export function setRevocationReadForTests(fn: ReadFn | undefined): void {
  readOverride = fn;
}

export async function isSessionRevoked(input: RevocationCheck): Promise<boolean> {
  try {
    const read = readOverride ?? getRevocation;
    const [sidHit, allAt] = await read({ sid: input.sid, userId: input.userId });
    if (sidHit !== null) return true;
    if (allAt !== null) {
      const epoch = Number(allAt);
      if (Number.isFinite(epoch) && input.iat < epoch) return true;
    }
    return false;
  } catch (err) {
    // Fail open: a Valkey hiccup must never become a mass sign-out.
    logger.warn({ err }, '[session-revocation] isSessionRevoked failed, failing open');
    return false;
  }
}
