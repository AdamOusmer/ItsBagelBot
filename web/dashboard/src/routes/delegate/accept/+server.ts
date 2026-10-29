// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { delegationGet } from '#lib/server/services';
import { cookieOptions, STATE_TTL_SECONDS } from '#lib/server/cookies';

export const GET: RequestHandler = async ({ url, cookies }) => {
  const token = url.searchParams.get('t');
  if (!token) throw redirect(302, '/login?e=link');

  const view = await delegationGet(token);
  if (!view || view.consumed) throw redirect(302, '/login?e=link');

  cookies.set('pending_delegation', token, cookieOptions(url, STATE_TTL_SECONDS));

  throw redirect(302, '/auth/login');
};
