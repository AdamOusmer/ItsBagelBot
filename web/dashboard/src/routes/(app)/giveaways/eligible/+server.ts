// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import { MOD } from '@bagel/kit';
import { effectiveId } from '#lib/server/board';
import { eligibleCount } from '#lib/server/channel-giveaways';
import { isPresetName } from '#lib/server/giveaway-input';
import { gateModulePage } from '#lib/server/module-gate';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ locals, url }) => {
  gateModulePage(locals.session, MOD.giveaway);
  const uid = effectiveId(locals.session);
  const preset = url.searchParams.get('preset') ?? '';
  if (preset && !isPresetName(preset)) return json(null, { status: 400 });
  return json(await eligibleCount(uid, preset).catch(() => null), { headers: { 'cache-control': 'no-store' } });
};
