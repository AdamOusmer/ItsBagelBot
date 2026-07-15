import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireAdmin } from '$lib/server/access';
import { shardSnapshot } from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

// Live snapshot poll target for the Shards page so shard state (connecting ->
// connected) and scale/delete results show in near-real-time without a manual
// page refresh.
export const GET: RequestHandler = async ({ locals }) => {
  if (!(await requireAdmin(locals.session))) throw error(403, 'forbidden');
  if (DEMO) {
    const { sampleSnapshot } = await import('$lib/server/demo-data');
    return json({ snapshot: sampleSnapshot });
  }
  try {
    return json({ snapshot: await shardSnapshot() });
  } catch (e) {
    return json({ snapshot: null, error: (e as Error).message });
  }
};
