// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { MOD } from '@bagel/kit';
import { env } from '@bagel/kit/server/env';
import { json } from '@sveltejs/kit';
import { effectiveId } from '#lib/server/board';
import { emptyLiveRaffles, liveDraws } from '#lib/server/channel-raffles';
import { gateModulePage } from '#lib/server/module-gate';
import type { RequestHandler } from './$types';

const DEMO = dev && env.DEMO === '1';
const headers = { 'cache-control': 'no-store' };

export const GET: RequestHandler = async ({ locals }) => {
  gateModulePage(locals.session, MOD.giveaway);
  if (DEMO) return json({ rounds: [], degraded: false, raffles: emptyLiveRaffles }, { headers });
  return json(await liveDraws(effectiveId(locals.session)), { headers });
};
