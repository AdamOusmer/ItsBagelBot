// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireAdmin } from '$lib/server/access';
import { shardSnapshot } from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

export const GET: RequestHandler = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw error(403, 'forbidden');
  if (DEMO) {
    const { sampleSnapshot } = await import('$lib/server/demo-data');
    return json({ snapshot: sampleSnapshot });
  }
  const snapshot = await shardSnapshot().catch(() => null);
  return json({ snapshot });
};
