// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

import type { RequestHandler } from './$types';
import { ready } from '@bagel/shared/server/nats';

export const GET: RequestHandler = async () => {
  if (!(await ready())) {
    return new Response('not ready', { status: 503, headers: { 'cache-control': 'no-store' } });
  }
  return new Response('ok', { headers: { 'cache-control': 'no-store' } });
};
