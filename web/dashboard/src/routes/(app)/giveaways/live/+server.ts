// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { MOD } from '@bagel/kit';
import { effectiveId } from '#lib/server/board';
import { liveGiveaways } from '#lib/server/channel-giveaways';
import { gateModulePage } from '#lib/server/module-gate';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ locals }) => {
  gateModulePage(locals.session, MOD.giveaway);
  return json(await liveGiveaways(effectiveId(locals.session)), { headers: { 'cache-control': 'no-store' } });
};
