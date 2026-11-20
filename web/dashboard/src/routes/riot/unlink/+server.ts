// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { logger } from '@bagel/kit/server/logger';
import { auditDashboardImpersonation } from '#lib/server/services';
import { clearRiotOAuthState, requireValorantActor, RIOT_MODULE_PATH, riotFail } from '#lib/server/riot-oauth';
import { riotLinkStore } from '#lib/server/riot-store';

export const POST: RequestHandler = async ({ cookies, locals }) => {
  const { uid } = requireValorantActor(locals);
  clearRiotOAuthState(cookies);
  try {
    await riotLinkStore(uid).clear();
  } catch (err) {
    logger.error({ err }, '[riot-unlink] link removal failed');
    riotFail('unlink', 303);
  }
  auditDashboardImpersonation(locals.session, 'riot:unlink', '');
  throw redirect(303, RIOT_MODULE_PATH);
};
