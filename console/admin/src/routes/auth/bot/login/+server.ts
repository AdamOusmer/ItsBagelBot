import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from 'arctic';
import { botTwitch, botScopes } from '$lib/server/oauth';
import { env } from '$env/dynamic/private';

// Start the bot-account authorization. Same cookie-state CSRF pattern as the
// operator /auth/login: the state cookie is set in whichever browser opens this
// URL, so the operator can copy the link and open it in the browser signed into
// the bot account; the callback validates the cookie in that same browser.
// force_verify makes Twitch always show the account picker so the right account
// consents.
export const GET: RequestHandler = ({ cookies, url }) => {
  if (!env.ADMIN_BOT_USER_ID?.trim()) {
    throw redirect(302, '/auth/bot/done?e=config');
  }

  const state = generateState();
  const authUrl = botTwitch(url.origin).createAuthorizationURL(state, botScopes());
  authUrl.searchParams.set('force_verify', 'true');

  cookies.set('bot_oauth_state', state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: 600
  });

  throw redirect(302, authUrl.toString());
};
