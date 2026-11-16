// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { count } from '@bagel/kit/server/analytics';

export const POST: RequestHandler = async ({ request }) => {
  const body = await request.json().catch(() => null);
  if (isBeacon(body)) count(body.event, body.dim);
  return new Response(null, { status: 204, headers: { 'Cache-Control': 'no-store' } });
};

function isBeacon(body: unknown): body is { event: string; dim: string } {
  if (!body || typeof body !== 'object') return false;
  const { event, dim } = body as Record<string, unknown>;
  const allowed = event === 'public.views' || event === 'marketing.cta';
  return allowed && typeof dim === 'string';
}
