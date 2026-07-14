import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { requireAdmin, isDemo } from '$lib/server/access';
import { serviceHealth } from '$lib/server/services';
import { sampleHealth } from '$lib/server/sample';

// Live poll target for the Overview health card: fresh RPC probes per call,
// so a recovering service turns green without a page reload.
export const GET: RequestHandler = async ({ locals }) => {
  if (!(await requireAdmin(locals.session))) throw error(403, 'forbidden');
  if (isDemo()) return json({ health: sampleHealth });
  return json({ health: await serviceHealth() });
};
