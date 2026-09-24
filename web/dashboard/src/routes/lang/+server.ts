// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { safeReturnPath } from '@bagel/kit/return-path';
import { redirect } from '@sveltejs/kit';
import { isLocale, LOCALE_COOKIE } from '@bagel/kit/i18n';
import { setLocale } from '$lib/server/services';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ request, url, cookies, locals }) => {
  const form = await request.formData();
  const to = String(form.get('to') ?? '');
  const next = String(form.get('next') ?? '/');

  if (isLocale(to)) {
    const s = locals.session;
    cookies.set(LOCALE_COOKIE, s?.impersonator_id ? 'en' : to, {
      path: '/',
      maxAge: 60 * 60 * 24 * 365,
      sameSite: 'lax',
      httpOnly: true,
      secure: url.protocol === 'https:'
    });

    if (s?.user_id) {
      await setLocale(s.user_id, to);
    }
  }

  const dest = safeReturnPath(next) ?? '/';
  throw redirect(303, dest);
};
