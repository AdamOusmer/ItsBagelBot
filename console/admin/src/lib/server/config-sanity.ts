import { assertCallback, assertOrigin } from '@bagel/shared/server/config-sanity';

type Env = Record<string, string | undefined>;

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
}
