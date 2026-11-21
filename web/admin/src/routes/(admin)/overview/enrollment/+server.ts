// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { error, json } from '@sveltejs/kit';
import { parseEnrollmentWindow } from '#lib/enrollment-window';
import { requireAdmin } from '#lib/server/access';
import { userEnrollment } from '#lib/server/services';
import type { RequestHandler } from './$types';

const DEMO = dev && process.env.DEMO === '1';

export const GET: RequestHandler = async ({ locals, url }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw error(403, 'forbidden');

  const days = parseEnrollmentWindow(url.searchParams.get('days'));
  if (DEMO) {
    const { demoEnrollment } = await import('#lib/server/demo-data');
    return json(demoEnrollment(days));
  }

  try {
    return json(await userEnrollment(admin.id, days));
  } catch {
    throw error(503);
  }
};
