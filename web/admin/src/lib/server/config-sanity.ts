// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  assertCallback,
  assertOrigin,
  positiveIntegerSetting
} from '@bagel/kit/server/config-sanity';
import { demoConfigured } from '@bagel/kit/server/demo-guard';

type Env = Record<string, string | undefined>;

export const DEFAULT_ADMIN_L1_CACHE_CAPACITY = 250;

export function assertDemoConfigSafe(env: Env): void {
  if (demoConfigured(env) && env.NODE_ENV === 'production') {
    throw new Error('DEMO must not be enabled in production');
  }
}

export function adminL1CacheCapacity(env: Env): number {
  return positiveIntegerSetting(
    'ADMIN_L1_CACHE_CAPACITY',
    env.ADMIN_L1_CACHE_CAPACITY,
    DEFAULT_ADMIN_L1_CACHE_CAPACITY
  );
}

export function assertConfigSane(env: Env): void {
  assertDemoConfigSafe(env);
  const origin = assertOrigin('ORIGIN', env.ORIGIN);
  assertCallback('TWITCH_REDIRECT_URI', env.TWITCH_REDIRECT_URI, {
    origin,
    callbackPath: '/auth/callback'
  });
  assertOrigin('DASHBOARD_PUBLIC_ORIGIN', env.DASHBOARD_PUBLIC_ORIGIN);
  if (env.BOT_REDIRECT_URI) {
    assertCallback('BOT_REDIRECT_URI', env.BOT_REDIRECT_URI, {
      origin,
      callbackPath: '/auth/bot/callback'
    });
  }
  adminL1CacheCapacity(env);
}
