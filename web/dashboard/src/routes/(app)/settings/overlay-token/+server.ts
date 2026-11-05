// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { moduleTokenStore, overlayConfigured, rotateOverlayToken } from '#lib/server/overlay-token';
import type { RequestHandler } from './$types';

function ownerId(locals: App.Locals): string | Response {
  const session = locals.session;
  if (!session) return json({ error: 'unauthorized' }, { status: 401 });
  if (session.delegate_of) return json({ error: 'forbidden' }, { status: 403 });
  return session.user_id;
}

const NO_STORE = { 'Cache-Control': 'no-store' };

export const GET: RequestHandler = async ({ locals }) => {
  const owner = ownerId(locals);
  if (typeof owner !== 'string') return owner;
  return json({ configured: await overlayConfigured(owner, moduleTokenStore) }, { headers: NO_STORE });
};

// A JSON body forces a CORS preflight, so another site cannot rotate a streamer's link with a bare form post.
export const POST: RequestHandler = async ({ locals, request, url }) => {
  const owner = ownerId(locals);
  if (typeof owner !== 'string') return owner;
  if (request.headers.get('content-type')?.split(';', 1)[0]?.trim() !== 'application/json') {
    return json({ error: 'unsupported' }, { status: 415 });
  }
  const token = await rotateOverlayToken(owner, moduleTokenStore);
  return json({ url: `${url.origin}/overlay/${token}` }, { headers: NO_STORE });
};
