// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import { requireAdmin } from '#lib/server/access';
import { CURSOR_COOKIE, preferenceCookieOptions } from '#lib/server/preference-cookies';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ request, cookies, url, locals }) => {
  if (!(await requireAdmin(locals.session))) error(403);

  const mediaType = request.headers.get('content-type')?.split(';', 1)[0].trim().toLowerCase();
  if (mediaType !== 'application/json') error(400);

  const body = (await request.json().catch(() => null)) as { enabled?: unknown } | null;
  if (typeof body?.enabled !== 'boolean') error(400);

  cookies.set(CURSOR_COOKIE, body.enabled ? '1' : '0', preferenceCookieOptions(url));
  return new Response(null, { status: 204 });
};
