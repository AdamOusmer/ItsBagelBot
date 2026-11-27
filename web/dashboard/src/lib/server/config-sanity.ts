// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  assertCallback,
  assertOptionalHTTPSURL,
  assertOrigin,
  positiveIntegerSetting
} from '@bagel/kit/server/config-sanity';
import { logger } from '@bagel/kit/server/logger';

type Env = Record<string, string | undefined>;

export const DEFAULT_DASHBOARD_L1_CACHE_CAPACITY = 5_000;

export function dashboardL1CacheCapacity(env: Env): number {
  return positiveIntegerSetting(
    'DASHBOARD_L1_CACHE_CAPACITY',
    env.DASHBOARD_L1_CACHE_CAPACITY,
    DEFAULT_DASHBOARD_L1_CACHE_CAPACITY
  );
}

const WEBHOOK_SECRET = /^whsec_[A-Za-z0-9+/]+={0,2}$/;

function warnMarketingConfig(env: Env): void {
  if (!env.BUSINESS_MAILING_ADDRESS?.trim()) {
    logger.warn('[startup] BUSINESS_MAILING_ADDRESS is empty. Marketing opt-in is disabled.');
  }
  const secret = env.RESEND_WEBHOOK_SECRET?.trim();
  if (secret && !WEBHOOK_SECRET.test(secret)) {
    logger.warn('[startup] RESEND_WEBHOOK_SECRET is not a whsec_ base64 secret. Resend webhooks will be rejected.');
  }
}

export function assertConfigSane(env: Env): void {
  const origin = env.ORIGIN ? assertOrigin('ORIGIN', env.ORIGIN) : undefined;
  assertCallback('TWITCH_REDIRECT_URI', env.TWITCH_REDIRECT_URI, {
    origin,
    callbackPath: '/auth/callback'
  });
  assertOptionalHTTPSURL('TEBEX_PREMIUM_CHECKOUT_URL', env.TEBEX_PREMIUM_CHECKOUT_URL);
  assertOptionalHTTPSURL('TEBEX_CANCEL_URL', env.TEBEX_CANCEL_URL);
  dashboardL1CacheCapacity(env);
  warnMarketingConfig(env);
}
