import { env } from '$env/dynamic/private';
import { assertCallback, assertOrigin } from '@bagel/shared/server/config-sanity';

export function assertConfigSane(): void {
  const origin = assertOrigin('ORIGIN', env.ORIGIN);
  assertCallback('TWITCH_REDIRECT_URI', env.TWITCH_REDIRECT_URI, {
    origin,
    callbackPath: '/auth/callback'
  });
}
