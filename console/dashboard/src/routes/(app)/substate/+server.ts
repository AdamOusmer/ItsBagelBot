// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { channelSubState } from '$lib/server/services';
import type { RequestHandler } from './$types';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Lightweight poll target: the dashboard fires a reconnect, then polls this
// until outgress flips the channel's enroll state off "pending" (to "ok" or
// "failing"). Kept tiny because the page hits it on a short interval.
export const GET: RequestHandler = async ({ locals }) => {
  const uid = locals.session?.user_id;
  if (!uid) return json({ state: 'unknown', error: '' }, { status: 401 });
  if (DEMO) return json({ state: 'ok', error: '' });
  return json(await channelSubState(uid));
};
