// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHash } from 'node:crypto';
import { ValkeyRateLimiter, type RateDecision } from '@bagel/kit/server/rate-limit';

// No app-level IP limit here: behind Traefik getClientAddress() is the proxy, so an IP bucket
// would make every desktop client share one. Per-IP limiting is the edge dashboard-ratelimit.
export function tokenRateKey(value: string): string {
  return createHash('sha256').update(value).digest('base64url').slice(0, 24);
}

const consentLimiter = new ValkeyRateLimiter({ name: 'oauth-desktop-consent', capacity: 20, refillPerSec: 20 / 60 });
const tokenKeyLimiter = new ValkeyRateLimiter({ name: 'oauth-desktop-token-key', capacity: 10, refillPerSec: 10 / 60 });

export function checkConsentRate(userId: string): Promise<RateDecision> {
  return consentLimiter.check(userId);
}

export function checkTokenRate(codeOrToken: string): Promise<RateDecision> {
  return tokenKeyLimiter.check(tokenRateKey(codeOrToken));
}
