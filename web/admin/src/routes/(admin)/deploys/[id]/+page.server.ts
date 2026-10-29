// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows, requireAdmin } from '#lib/server/access';
import { loadDeployRun, runActions } from '#lib/server/deploys';

export const load: PageServerLoad = async ({ locals, params }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');
  if (!allows(admin.role, 'deploys.manage')) throw redirect(302, '/');
  return loadDeployRun({ id: admin.id }, params.id);
};

export const actions: Actions = runActions;
