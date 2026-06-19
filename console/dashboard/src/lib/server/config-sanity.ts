import { assertCallback, assertOrigin } from '@bagel/shared/server/config-sanity';

export function assertConfigSane(): void {
  const origin = assertOrigin('ORIGIN', process.env.ORIGIN);
  assertCallback('TWITCH_REDIRECT_URI', process.env.TWITCH_REDIRECT_URI, {
    origin,
    callbackPath: '/auth/callback'
  });
}
