// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { text } from '@sveltejs/kit';
import { setMarketingLink } from '#lib/server/services';
import { linkFailure, oneClickRequest } from '#lib/server/unsubscribe';
import type { RequestHandler } from './$types';

const PLAIN = { 'content-type': 'text/plain; charset=utf-8', 'cache-control': 'no-store' };

export const POST: RequestHandler = async ({ request, params }) => {
  if (!(await oneClickRequest(request))) return text('Bad request', { status: 400, headers: PLAIN });
  try {
    await setMarketingLink({ token: params.token, status: 'withdrawn', source: 'one_click' });
  } catch (err) {
    const invalid = linkFailure(err) === 'invalid';
    return text(invalid ? 'Not found' : 'Unavailable', { status: invalid ? 404 : 503, headers: PLAIN });
  }
  return text('Unsubscribed', { status: 200, headers: PLAIN });
};
