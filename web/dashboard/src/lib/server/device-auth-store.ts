// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { ValkeyRateLimiter, type RateDecision } from '@bagel/kit/server/rate-limit';

const consentLimiter = new ValkeyRateLimiter({ name: 'oauth-desktop-consent', capacity: 20, refillPerSec: 20 / 60 });

export function checkConsentRate(userId: string): Promise<RateDecision> {
  return consentLimiter.check(userId);
}
