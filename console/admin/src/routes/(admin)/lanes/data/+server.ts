import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { requireAdmin } from '$lib/server/access';
import { loadLanes } from '$lib/server/lanes';

// Live poll target for the Lanes page: the sampler keeps a warm snapshot, so
// this answers from memory and refreshes in the background.
export const GET: RequestHandler = async ({ locals }) => {
  if (!(await requireAdmin(locals.session))) throw error(403, 'forbidden');
  return json(await loadLanes());
};
