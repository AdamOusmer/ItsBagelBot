import { redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';
import type { Session } from '$lib/server/session';
import { isBanned } from '$lib/server/rpc';

// Demo session lets the app render without the Twitch OAuth flow wired up yet.
// Off unless DEMO=1; production paths require a real encrypted session.
const demo: Session = {
  user_id: 'demo',
  login: 'itsmavey',
  display_name: 'Mavey',
  role: 'streamer',
  expires_at: Math.floor(Date.now() / 1000) + 3600
};

export const load: LayoutServerLoad = async ({ locals }) => {
  let s = locals.session;
  if (!s && env.DEMO === '1') s = demo;
  if (!s) throw redirect(302, '/login');

  // Defense in depth: bounce a session whose user was banned after sign-in.
  // isBanned fails open so an RPC blip never locks out the whole app.
  if (env.DEMO !== '1' && (await isBanned(s.user_id))) throw redirect(302, '/login?e=banned');

  return {
    role: s.role,
    displayName: s.display_name,
    login: s.login,
    impersonatorLogin: s.impersonator_id ? s.impersonator_login : undefined
  };
};
