import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { COOKIE } from '$lib/server/session';

// Clears the admin session and returns to the login screen. POST so a stray
// GET (prefetch / crawler) cannot log the operator out.
export const POST: RequestHandler = ({ cookies }) => {
  cookies.delete(COOKIE, { path: '/' });
  throw redirect(302, '/login');
};
