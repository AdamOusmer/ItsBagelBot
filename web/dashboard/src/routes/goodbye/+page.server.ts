// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { ACCOUNT_DELETED_COOKIE } from '#lib/server/session';
import { deleteOptions } from '#lib/server/cookies';

export const load: PageServerLoad = ({ locals, cookies, url }) => {
  const wasDeleted = cookies.get(ACCOUNT_DELETED_COOKIE) === '1';
  cookies.delete(ACCOUNT_DELETED_COOKIE, deleteOptions(url));

  if (locals.session) throw redirect(302, '/');
  if (!wasDeleted) throw redirect(302, '/login');

  return {};
};
