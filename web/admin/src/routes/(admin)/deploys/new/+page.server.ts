// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows, requireAdmin } from '$lib/server/access';
import { loadDeploys } from '$lib/server/deploys';

export const load: PageServerLoad = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');
  if (!allows(admin.role, 'deploys.manage')) throw redirect(302, '/');
  const { planned, active } = await loadDeploys({ id: admin.id });
  if (active) throw redirect(303, `/deploys/${encodeURIComponent(active.id)}`);
  return planned;
};
