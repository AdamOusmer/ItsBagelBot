// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows } from '$lib/server/access';
import { deployActions, loadDeploys } from '$lib/server/deploys';

// Awaited, not streamed: the page's start form is built from the plan, and a
// form that fills in after the operator starts reading it is the layout shift
// the console avoids. A slow or failed plan still renders, with planError.
export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  if (!allows(layout.role, 'deploys.manage')) throw redirect(302, '/');
  return loadDeploys({ id: layout.id });
};

// Every action re-checks the role itself (requireRole in the shared gate); the
// load's redirect only decides whether the page renders.
export const actions: Actions = deployActions;
