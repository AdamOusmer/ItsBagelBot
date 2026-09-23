// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Starts the Nightbot OAuth connect flow for the config importer: mints the
// CSRF state cookie and bounces to nightbot.tv's consent screen. Owner-only,
// same policy as the import actions themselves: an import rewrites the
// board wholesale and delegates are read-mostly by design.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { randomBytes } from 'node:crypto';
import {
  NB_RETURN_COOKIE,
  NB_STATE_COOKIE_PATH,
  NB_STATE_COOKIE,
  importOwner,
  nightbotAuthURL,
  nightbotConfigured
} from '$lib/server/nightbot-oauth';

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  if (!importOwner(locals)) throw redirect(302, '/');
  const welcome = url.searchParams.get('return') === 'welcome';
  const wizard = welcome ? '/welcome?import=1&source=nightbot' : '/settings/import?source=nightbot';
  if (!nightbotConfigured()) throw redirect(302, `${wizard}&e=nb_config`);

  const state = randomBytes(16).toString('base64url');
  cookies.set(NB_STATE_COOKIE, state, {
    path: NB_STATE_COOKIE_PATH,
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: 600
  });
  if (welcome) {
    cookies.set(NB_RETURN_COOKIE, 'welcome', {
      path: NB_STATE_COOKIE_PATH,
      httpOnly: true,
      secure: url.protocol === 'https:',
      sameSite: 'lax',
      maxAge: 600
    });
  } else {
    cookies.delete(NB_RETURN_COOKIE, { path: NB_STATE_COOKIE_PATH });
  }
  throw redirect(302, nightbotAuthURL(state).toString());
};
