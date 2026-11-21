// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, redirect } from '@sveltejs/kit';
import { isLocale, LOCALE_COOKIE } from '@bagel/kit/i18n';
import { safeReturnPath } from '@bagel/kit/return-path';
import { requireAdmin } from '#lib/server/access';
import { preferenceCookieOptions } from '#lib/server/preference-cookies';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ request, cookies, url, locals }) => {
  if (!(await requireAdmin(locals.session))) error(403);

  const form = await request.formData().catch(() => null);
  const locale = form?.get('to');
  if (!isLocale(locale)) error(400);

  const next = form?.get('next');
  const destination = typeof next === 'string' ? safeReturnPath(next) : null;
  cookies.set(LOCALE_COOKIE, locale, preferenceCookieOptions(url));
  redirect(303, destination ?? '/');
};
