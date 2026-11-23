// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { error, json } from '@sveltejs/kit';
import { requireAdmin } from '#lib/server/access';
import { readShardSnapshot } from '#lib/components/shards/fleet-read.server';

export const GET: RequestHandler = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw error(403, 'forbidden');
  const snapshot = await readShardSnapshot().catch(() => null);
  return json({ snapshot }, {
    status: snapshot ? 200 : 503,
    headers: { 'cache-control': 'no-store' }
  });
};
