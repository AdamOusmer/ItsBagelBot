import { json } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import { listCounters } from '$lib/server/loyalty-store';
import type { RequestHandler } from './$types';

// Feeds the editors' counter picker (commands / channel points) without
// threading the counter list through every page load. Session-gated only:
// a delegate editing commands may insert counters even without the modules
// grant — reading names is harmless, management stays on /counters.
export const GET: RequestHandler = async ({ locals }) => {
  const uid = locals.session?.delegate_of ?? locals.session?.user_id;
  if (env.DEMO === '1') return json({ counters: [] });
  if (!uid) return json({ counters: [] }, { status: 401 });
  try {
    return json({ counters: await listCounters(uid) });
  } catch {
    return json({ counters: [] }, { status: 503 });
  }
};
