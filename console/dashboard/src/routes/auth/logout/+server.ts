import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { COOKIE } from '$lib/server/session';

export const POST: RequestHandler = ({ cookies, url }) => {
  cookies.delete(COOKIE, { path: '/', secure: url.protocol === 'https:' });
  throw redirect(302, '/login');
};
