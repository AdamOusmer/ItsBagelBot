// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { logger } from './logger';
import { getRevocation, sessionRevokedAllKey, sessionRevokedKey } from './valkey-store';
import { masterClient, warmMasterClient } from './valkey-master';
import { CircuitBreaker, withTimeout } from './resilience';
import { RevocationCache, type LocalRevoke } from './revocation-cache';

const OP_TIMEOUT_MS = 200;

const writeBreaker = new CircuitBreaker({ name: 'valkey-session-revocation', failureThreshold: 3, resetMs: 5_000 });

const cache = new RevocationCache();

export function warmSessionRevocation(): void {
  warmMasterClient();
}

interface RevocationWrite {
  local: LocalRevoke;
  valkeyKey: string;
  value: string;
  ttlSeconds: number;
  failure: string;
}

async function persistRevocation(write: RevocationWrite): Promise<void> {
  cache.remember(write.local);
  const client = masterClient();
  if (!client) return;
  try {
    await writeBreaker.run(() =>
      withTimeout(
        client.set(write.valkeyKey, write.value, 'EX', Math.max(1, write.ttlSeconds)),
        OP_TIMEOUT_MS,
        'valkey session-revocation write'
      )
    );
  } catch (err) {
    logger.warn({ err }, write.failure);
  }
}

export function revokeSession(sid: string, ttlSeconds: number): Promise<void> {
  return persistRevocation({
    local: { key: `sid:${sid}`, epoch: Number.POSITIVE_INFINITY },
    valkeyKey: sessionRevokedKey(sid),
    value: '1',
    ttlSeconds,
    failure: '[session-revocation] revokeSession failed'
  });
}

export function revokeAllForUser(userId: string, atUnixSeconds: number, ttlSeconds: number): Promise<void> {
  return persistRevocation({
    local: { key: `all:${userId}`, epoch: atUnixSeconds },
    valkeyKey: sessionRevokedAllKey(userId),
    value: String(atUnixSeconds),
    ttlSeconds,
    failure: '[session-revocation] revokeAllForUser failed'
  });
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
  cache.clear();
}

export async function isSessionRevoked(input: RevocationCheck): Promise<boolean> {
  if (cache.revokedHere(input)) return true;
  try {
    const [sidHit, allAt] = await cache.read({ sid: input.sid, userId: input.userId }, readOverride ?? getRevocation);
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
