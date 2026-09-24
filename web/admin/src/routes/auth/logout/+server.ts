// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { COOKIE } from '$lib/server/session';

export const POST: RequestHandler = ({ cookies }) => {
  cookies.delete(COOKIE, { path: '/' });
  throw redirect(302, '/login');
};
