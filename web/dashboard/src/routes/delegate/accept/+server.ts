// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { delegationGet } from '$lib/server/services';

export const GET: RequestHandler = async ({ url, cookies }) => {
  const token = url.searchParams.get('t');
  if (!token) throw redirect(302, '/login?e=link');

  const view = await delegationGet(token);
  if (!view || view.consumed) throw redirect(302, '/login?e=link');

  cookies.set('pending_delegation', token, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: 600
  });

  throw redirect(302, '/auth/login');
};
