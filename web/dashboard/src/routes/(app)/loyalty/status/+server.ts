// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { json } from '@sveltejs/kit';
import { effectiveId } from '#lib/server/board';
import { raffleStatus } from '#lib/server/loyalty-store';
import { gateModulePage } from '#lib/server/module-gate';
import type { RequestHandler } from './$types';

const DEMO = dev && env.DEMO === '1';
const headers = { 'cache-control': 'no-store' };

export const GET: RequestHandler = async ({ locals }) => {
  gateModulePage(locals.session, 'loyalty');
  if (DEMO) return json(null, { headers });
  if (!locals.session) return json(null, { status: 401, headers });
  const latest = await raffleStatus(effectiveId(locals.session)).catch(() => null);
  return json(latest, { headers });
};
