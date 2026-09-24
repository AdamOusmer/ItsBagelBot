// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { randomBytes } from 'node:crypto';
import newrelic from 'newrelic';
import { verifyViewAs } from '@bagel/kit/server/impersonation';
import { claimOnce } from '@bagel/kit/server/rate-limit';
import { COOKIE, seal, IMPERSONATION_TTL_SECONDS } from '$lib/server/session';
import { LOCALE_COOKIE } from '@bagel/kit/i18n';

const JTI_TTL_SECONDS = 6 * 60;

export const GET: RequestHandler = async ({ url, cookies }) => {
  const token = url.searchParams.get('t') ?? '';
  const p = verifyViewAs(token);
  if (!p) throw redirect(302, '/login?e=imp');

  const claim = await claimOnce(`viewas:jti:${p.jti}`, JTI_TTL_SECONDS);
  if (claim === 'replayed' || claim === 'unavailable') {
    newrelic.addCustomAttributes({ 'viewas.claim': claim, 'viewas.by': p.by_id });
  }
  if (claim === 'replayed') {
    throw redirect(302, '/login?e=imp');
  }

  const now = Math.floor(Date.now() / 1000);
  const value = seal({
    user_id: p.sub,
    login: p.login,
    display_name: p.display_name,
    role: 'streamer',
    sid: randomBytes(16).toString('base64url'),
    iat: now,
    expires_at: now + IMPERSONATION_TTL_SECONDS,
    impersonator_id: p.by_id,
    impersonator_login: p.by_login
  });

  cookies.set(COOKIE, value, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: IMPERSONATION_TTL_SECONDS
  });

  cookies.set(LOCALE_COOKIE, 'en', {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: 60 * 60 * 24 * 365
  });

  throw redirect(302, '/');
};
