import { json } from '@sveltejs/kit';
import { env } from '$env/dynamic/private';
import { listCounters } from '$lib/server/loyalty-store';
import type { RequestHandler } from './$types';

// Feeds the editors' counter picker (commands / channel points) without
// threading the counter list through every page load. Session-gated, and
// open to commands-only delegates via guard.ts's /counters/list carve-out,
// so the reply carries names and scopes only: values are channel metrics
// and stay on the modules-gated /counters page.
export const GET: RequestHandler = async ({ locals }) => {
  const uid = locals.session?.delegate_of ?? locals.session?.user_id;
  if (env.DEMO === '1') return json({ counters: [] });
  if (!uid) return json({ counters: [] }, { status: 401 });
  try {
    const counters = (await listCounters(uid)).map(({ name, scope }) => ({ name, scope }));
    return json({ counters });
  } catch {
    return json({ counters: [] }, { status: 503 });
  }
};
