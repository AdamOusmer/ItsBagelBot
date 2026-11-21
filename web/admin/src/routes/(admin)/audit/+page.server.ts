// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows } from '#lib/access';
import { auditQuery, loadAuditPage } from '#lib/components/audit/audit-page.server';
import { AUDIT_MAX_PAGES, AUDIT_PAGE_SIZE } from '#lib/server/services';

export const load: PageServerLoad = async ({ parent, url }) => {
  const admin = await parent();
  if (!allows(admin.role, 'audit.read')) throw redirect(302, '/');

  const query = auditQuery(url.searchParams);
  return {
    ...query,
    pageSize: AUDIT_PAGE_SIZE,
    maxPages: AUDIT_MAX_PAGES,
    firstPage: loadAuditPage(query)
  };
};
