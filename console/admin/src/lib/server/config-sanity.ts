import {
  assertCallback,
  assertOrigin,
  positiveIntegerSetting
} from '@bagel/shared/server/config-sanity';

type Env = Record<string, string | undefined>;

// Admin has a small operator-only working set, mostly snapshots and short-lived
// user/page reads, so it needs substantially fewer resident entries than the
// public dashboard.
export const DEFAULT_ADMIN_L1_CACHE_CAPACITY = 250;

export function adminL1CacheCapacity(env: Env): number {
  return positiveIntegerSetting(
    'ADMIN_L1_CACHE_CAPACITY',
    env.ADMIN_L1_CACHE_CAPACITY,
    DEFAULT_ADMIN_L1_CACHE_CAPACITY
  );
}

// Validate at boot (from the init hook), reading the injected env rather than
// process.env so all runtime config flows through $env/dynamic/private.
export function assertConfigSane(env: Env): void {
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
