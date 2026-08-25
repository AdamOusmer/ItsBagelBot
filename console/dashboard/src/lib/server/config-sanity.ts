// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { demoConfigured } from '@bagel/shared/server/demo-guard';
import {
  assertCallback,
  assertOptionalHTTPSURL,
  assertOrigin,
  positiveIntegerSetting
} from '@bagel/shared/server/config-sanity';

type Env = Record<string, string | undefined>;

// A dashboard board holds roughly a dozen independently invalidated key
// families. 1,000 entries leaves room for the active per-pod working set while
// bounding warmed-at-rest memory far below SwrCache's general-purpose default.
export const DEFAULT_DASHBOARD_L1_CACHE_CAPACITY = 1_000;

// DEMO is a local-development feature. Reject any runtime that tries to enable
// it on a build that compiled every demo branch out: a DEMO=1 there means an
// operator believes this pod is a demo instance when it cannot be one.
//
// The condition is the build-time `dev` constant, NOT NODE_ENV. NODE_ENV is
// just another env var: a container started with NODE_ENV=development would
// slip past a NODE_ENV check while still running the stripped production
// bundle. `dev` is baked in at build time and no environment can move it.
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

// Validate at boot (from the init hook), reading the injected env rather than
// process.env so all runtime config flows through $env/dynamic/private.
export function assertConfigSane(env: Env): void {
  assertDemoConfigSafe(env);
  // ORIGIN is optional in production: the app answers under two hostnames
  // (dashboard. and stats.itsbagelbot.com), so the deployment derives each
  // request's origin from traefik's forwarded headers (HOST_HEADER /
  // PROTOCOL_HEADER) instead of pinning one. When ORIGIN is set (dev, DEMO),
  // it must still be a bare origin and must agree with the callback.
  const origin = env.ORIGIN ? assertOrigin('ORIGIN', env.ORIGIN) : undefined;
  assertCallback('TWITCH_REDIRECT_URI', env.TWITCH_REDIRECT_URI, {
    origin,
    callbackPath: '/auth/callback'
  });
  // The YouTube connect flow is optional at boot: any GOOGLE_* var present
  // requires all three, and the callback must point at /youtube/callback.
  if (env.GOOGLE_CLIENT_ID || env.GOOGLE_CLIENT_SECRET || env.GOOGLE_REDIRECT_URI) {
    if (!env.GOOGLE_CLIENT_ID || !env.GOOGLE_CLIENT_SECRET || !env.GOOGLE_REDIRECT_URI) {
      throw new Error('GOOGLE_CLIENT_ID/SECRET/REDIRECT_URI must be set together');
    }
    assertCallback('GOOGLE_REDIRECT_URI', env.GOOGLE_REDIRECT_URI, {
      origin,
      callbackPath: '/youtube/callback'
    });
  }
  assertOptionalHTTPSURL('TEBEX_PREMIUM_CHECKOUT_URL', env.TEBEX_PREMIUM_CHECKOUT_URL);
  assertOptionalHTTPSURL('TEBEX_CANCEL_URL', env.TEBEX_CANCEL_URL);
  dashboardL1CacheCapacity(env);
}
