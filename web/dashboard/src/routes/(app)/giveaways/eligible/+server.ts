// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { MOD } from '@bagel/kit';
import { env } from '@bagel/kit/server/env';
import { json } from '@sveltejs/kit';
import { effectiveId } from '#lib/server/board';
import { eligibleCount } from '#lib/server/channel-giveaways';
import { isPresetName } from '#lib/server/giveaway-input';
import { gateModulePage } from '#lib/server/module-gate';
import type { RequestHandler } from './$types';

const DEMO = dev && env.DEMO === '1';
const headers = { 'cache-control': 'no-store' };

export const GET: RequestHandler = async ({ locals, url }) => {
  gateModulePage(locals.session, MOD.giveaway);
  if (DEMO) return json(null, { headers });
  const preset = url.searchParams.get('preset') ?? '';
  if (preset && !isPresetName(preset)) return json(null, { status: 400 });
  return json(await eligibleCount(effectiveId(locals.session), preset).catch(() => null), { headers });
};
