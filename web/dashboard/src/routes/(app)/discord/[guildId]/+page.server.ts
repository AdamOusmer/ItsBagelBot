// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { logger } from '@bagel/kit/server/logger';
import type { PageServerLoad } from './$types';
import { effectiveId } from '$lib/server/board';
import { listGuildModules } from '$lib/server/discord-store';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

// Enabled flag per stored module row; modules without a row are absent and follow their kind default.
export const load: PageServerLoad = async ({ locals, params }) => {
  const stored: Record<string, boolean> = {};
  if (DEMO) return { stored, modulesDegraded: false };
  try {
    const list = await listGuildModules({ userId: effectiveId(locals.session), guildId: params.guildId });
    for (const [id, row] of Object.entries(list.modules)) stored[id] = row.enabled;
    return { stored, modulesDegraded: false };
  } catch (e) {
    logger.warn({ err: e }, '[discord] module states unavailable');
    return { stored, modulesDegraded: true };
  }
};
