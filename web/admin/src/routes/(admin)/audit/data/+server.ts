// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { requireRole } from '#lib/server/access';
import { auditQuery, loadAuditPage } from '#lib/components/audit/audit-page.server';

export const GET: RequestHandler = async ({ url, locals }) => {
  const admin = await requireRole({ locals }, 'audit.read');
  if (!admin) throw error(403, 'forbidden');

  return json(await loadAuditPage(auditQuery(url.searchParams)));
};
