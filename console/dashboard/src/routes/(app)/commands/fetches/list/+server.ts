// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { listFetches } from '$lib/server/fetches-store';
import type { RequestHandler } from './$types';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Feeds the response editor's {urlfetch:name} chip panel without threading
// the def list through every page load (the /counters/list precedent).
// Session-gated; delegates scoped to commands already reach /commands/* via
// guard.ts's section prefix. Names only — URLs and paths stay on the fetches
// page, key material exists on no read path at all.
export const GET: RequestHandler = async ({ locals }) => {
  const uid = locals.session?.delegate_of ?? locals.session?.user_id;
  if (DEMO) return json({ defs: [] });
  if (!uid) return json({ defs: [] }, { status: 401 });
  try {
    const defs = (await listFetches(uid)).defs.map(({ name }) => ({ name }));
    return json({ defs });
  } catch {
    return json({ defs: [] }, { status: 503 });
  }
};
