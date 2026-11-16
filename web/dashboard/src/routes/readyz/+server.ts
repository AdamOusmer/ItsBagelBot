// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { ready } from '@bagel/kit/server/nats';
import { ready as warmValkey } from '@bagel/kit/server/valkey-store';
import { rateLimiterReady } from '@bagel/kit/server/rate-limit';

export const GET: RequestHandler = async () => {
  // NATS is the only readiness gate: a Valkey outage must not pull every replica out of rotation.
  const [natsReady] = await Promise.all([ready(), warmValkey(), rateLimiterReady()]);
  if (!natsReady) {
    return new Response('not ready', { status: 503, headers: { 'cache-control': 'no-store' } });
  }
  return new Response('ok', { headers: { 'cache-control': 'no-store' } });
};
