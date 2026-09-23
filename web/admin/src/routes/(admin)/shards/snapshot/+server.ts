// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { allows, requireAdmin } from '$lib/server/access';
import { shardSnapshot, trialList } from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

// Live snapshot poll target for the Shards page so shard state (connecting ->
// connected) and scale/delete results show in near-real-time without a manual
// page refresh.
export const GET: RequestHandler = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw error(403, 'forbidden');
  if (DEMO) {
    const { sampleSnapshot } = await import('$lib/server/demo-data');
    return json({ snapshot: sampleSnapshot, trials: null });
  }
  const [snapshot, trials] = await Promise.all([
    shardSnapshot().catch(() => null),
    (allows(admin.role, 'trials.manage') ? trialList() : Promise.resolve(null)).catch(() => null)
  ]);
  return json({ snapshot, trials });
};
