// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { json } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { gateModulePage } from '$lib/server/module-gate';
import { effectiveId } from '$lib/server/board';
import { readQueueView } from '$lib/server/songqueue-live';

const DEMO = dev && env.DEMO === '1';

export const GET: RequestHandler = async ({ locals }) => {
  gateModulePage(locals.session, 'songqueue');
  if (DEMO) return json({ queue: null });
  if (!locals.session) return json({ queue: null }, { status: 401 });
  const queue = await readQueueView(effectiveId(locals.session));
  return json({ queue }, { headers: { 'cache-control': 'no-store' } });
};
