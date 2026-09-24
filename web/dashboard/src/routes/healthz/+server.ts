// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';

export const GET: RequestHandler = () =>
  new Response('ok', { headers: { 'cache-control': 'no-store' } });
