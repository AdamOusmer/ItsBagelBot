// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows } from '#lib/server/access';
import { deployActions, deployApi } from '#lib/server/deploys';
import { readDeployFlux, readDeployPlan, readDeployRuns } from '#lib/server/deploy-reads';

export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  if (!allows(layout.role, 'deploys.manage')) throw redirect(302, '/');
  const api = deployApi();
  const actor = { actor_id: layout.id };
  return {
    runs: readDeployRuns(api, actor),
    planned: readDeployPlan(api, actor),
    flux: readDeployFlux()
  };
};

export const actions: Actions = deployActions;
