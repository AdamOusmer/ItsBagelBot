// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { listCounters } from '$lib/server/loyalty-store';
import type { RequestHandler } from './$types';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// delegateBlocked answers the one question this endpoint asks of a delegated
// session: does their grant own a counter picker at all? Either grant that
// carries a counter editor opens it — commands (command replies) or modules
// (the counters page itself). An owner session is never blocked.
function delegateBlocked(s: App.Locals['session']): boolean {
  if (!s?.delegate_of) return false;
  const sections = s.sections ?? [];
  return !sections.includes('commands') && !sections.includes('modules');
}

// Feeds the editors' counter picker (commands / channel points) without
// threading the counter list through every page load. Session-gated, and
// open to commands-only delegates via guard.ts's /counters/list carve-out,
// so the reply carries names and scopes only: values are channel metrics
// and stay on the modules-gated /counters page.
export const GET: RequestHandler = async ({ locals }) => {
  const s = locals.session;
  if (delegateBlocked(s)) return json({ counters: [] }, { status: 403 });
  const uid = s?.delegate_of ?? s?.user_id;
  if (DEMO) return json({ counters: [] });
  if (!uid) return json({ counters: [] }, { status: 401 });
  try {
    const counters = (await listCounters(uid)).map(({ name, scope }) => ({ name, scope }));
    return json({ counters });
  } catch {
    return json({ counters: [] }, { status: 503 });
  }
};
