// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows } from '$lib/server/access';
import { loadDeployRun, runActions } from '$lib/server/deploys';

export const load: PageServerLoad = async ({ parent, params }) => {
  const layout = await parent();
  if (!allows(layout.role, 'deploys.manage')) throw redirect(302, '/');
  return loadDeployRun({ id: layout.id }, params.id);
};

export const actions: Actions = runActions;
