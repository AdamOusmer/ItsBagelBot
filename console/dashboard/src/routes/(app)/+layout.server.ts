import { redirect } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import type { LayoutServerLoad } from './$types';
import type { Session } from '$lib/server/session';

// Demo session lets the app render without the Twitch OAuth flow wired up yet.
// Off unless DEMO=1; production paths require a real encrypted session.
const demo: Session = {
  user_id: 'demo',
  login: 'itsmavey',
  display_name: 'Mavey',
  role: 'streamer',
  expires_at: Math.floor(Date.now() / 1000) + 3600
};

export const load: LayoutServerLoad = ({ locals }) => {
  let s = locals.session;
  if (!s && env.DEMO === '1') s = demo;
  if (!s) throw redirect(302, '/login');
  return { role: s.role, displayName: s.display_name, login: s.login };
};
