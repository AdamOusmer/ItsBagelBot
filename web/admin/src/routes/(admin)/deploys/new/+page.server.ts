// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows, requireAdmin } from '#lib/server/access';
import { releaseApi } from '#lib/server/release';
import { readDeployPlan, readDeployRuns } from '#lib/server/deploy-reads';

export const load: PageServerLoad = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');
  if (!allows(admin.role, 'deploys.manage')) throw redirect(302, '/');
  const actor = { actor_id: admin.id };
  const planned = readDeployPlan(releaseApi, actor);
  const { active } = await readDeployRuns(releaseApi, actor);
  if (active) throw redirect(303, `/deploys/${encodeURIComponent(active.id)}`);
  return { planned };
};
