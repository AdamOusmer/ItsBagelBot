// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows } from '$lib/server/access';
import { AUDIT_MAX_PAGES, AUDIT_PAGE_SIZE } from '$lib/server/services';
import { parsePage, normalizeSearch } from '$lib/server/paging';

export const load: PageServerLoad = async ({ parent, url }) => {
  const admin = await parent();
  // The audit trail is sensitive (who did what); managers only.
  if (!allows(admin.role, 'audit.read')) throw redirect(302, '/');

  const page = parsePage(url.searchParams.get('page'), AUDIT_MAX_PAGES);
  const search = normalizeSearch(url.searchParams.get('q'));

  return {
    page,
    pageSize: AUDIT_PAGE_SIZE,
    maxPages: AUDIT_MAX_PAGES,
    search
  };
};
