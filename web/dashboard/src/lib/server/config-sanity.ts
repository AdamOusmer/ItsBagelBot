// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { demoConfigured } from '@bagel/kit/server/demo-guard';
import {
  assertCallback,
  assertOptionalHTTPSURL,
  assertOrigin,
  positiveIntegerSetting
} from '@bagel/kit/server/config-sanity';

type Env = Record<string, string | undefined>;

export const DEFAULT_DASHBOARD_L1_CACHE_CAPACITY = 1_000;

// Check the build-time `dev`, never NODE_ENV: any container can set NODE_ENV.
export function assertDemoConfigSafe(env: Env): void {
  if (!dev && demoConfigured(env)) {
    throw new Error('DEMO must not be enabled in production');
  }
}

export function dashboardL1CacheCapacity(env: Env): number {
  return positiveIntegerSetting(
    'DASHBOARD_L1_CACHE_CAPACITY',
    env.DASHBOARD_L1_CACHE_CAPACITY,
    DEFAULT_DASHBOARD_L1_CACHE_CAPACITY
  );
}

export function assertConfigSane(env: Env): void {
  assertDemoConfigSafe(env);
  const origin = env.ORIGIN ? assertOrigin('ORIGIN', env.ORIGIN) : undefined;
  assertCallback('TWITCH_REDIRECT_URI', env.TWITCH_REDIRECT_URI, {
    origin,
    callbackPath: '/auth/callback'
  });
  assertOptionalHTTPSURL('TEBEX_PREMIUM_CHECKOUT_URL', env.TEBEX_PREMIUM_CHECKOUT_URL);
  assertOptionalHTTPSURL('TEBEX_CANCEL_URL', env.TEBEX_CANCEL_URL);
  dashboardL1CacheCapacity(env);
}
