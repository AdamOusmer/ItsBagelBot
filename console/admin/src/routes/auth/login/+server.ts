import { redirect } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { env } from '$env/dynamic/private';

// The admin panel does not run its own OAuth flow; it consumes the shared
// encrypted session minted by the dashboard tier. Send operators to the
// dashboard login (DASHBOARD_LOGIN_URL), then they return with a session cookie
// the allowlist check validates. Falls back to /login when unconfigured.
export const GET: RequestHandler = () => {
  const dest = env.DASHBOARD_LOGIN_URL;
  if (dest) throw redirect(302, dest);
  throw redirect(302, '/login');
};
