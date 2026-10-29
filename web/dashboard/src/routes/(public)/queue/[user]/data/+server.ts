// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { readQueueChannel, requireQueueLogin } from '#lib/server/queue-channel';
import { QUEUE_EDGE_HEADERS, QUEUE_UNCACHED_HEADERS } from '../edge-headers';

export const GET: RequestHandler = async ({ params, url, locals }) => {
  const login = requireQueueLogin(url, params.user ?? '', locals);
  const read = await readQueueChannel(login, locals);
  return json(read.snapshot, { headers: read.outcome === 'page' ? QUEUE_EDGE_HEADERS : QUEUE_UNCACHED_HEADERS });
};
