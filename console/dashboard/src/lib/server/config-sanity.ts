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
  const origin = assertOrigin('ORIGIN', env.ORIGIN);
  assertCallback('TWITCH_REDIRECT_URI', env.TWITCH_REDIRECT_URI, {
    origin,
    callbackPath: '/auth/callback'
  });
  assertOptionalHTTPSURL('TEBEX_PREMIUM_CHECKOUT_URL', env.TEBEX_PREMIUM_CHECKOUT_URL);
  assertOptionalHTTPSURL('TEBEX_CANCEL_URL', env.TEBEX_CANCEL_URL);
  dashboardL1CacheCapacity(env);
}
