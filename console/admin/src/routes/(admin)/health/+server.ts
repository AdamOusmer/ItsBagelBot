import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireAdmin } from '$lib/server/access';
import { serviceHealth } from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

// Live poll target for the Analytics RPC timing panel: fresh probes per call,
// so a recovering service turns green without a page reload.
export const GET: RequestHandler = async ({ locals }) => {
  if (!(await requireAdmin(locals.session))) throw error(403, 'forbidden');
  if (DEMO) {
    const { sampleHealth } = await import('$lib/server/demo-data');
    return json({ health: sampleHealth });
  }
  return json({ health: await serviceHealth() });
};
