// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { normalizeSearch, parsePage } from '#lib/server/paging';
import { auditPage, AUDIT_MAX_PAGES, AUDIT_PAGE_SIZE, type AuditPage } from '#lib/server/services';

type AuditQuery = { page: number; search: string };
export type AuditPageResult = AuditPage & { error?: string };

export function auditQuery(params: URLSearchParams): AuditQuery {
  return {
    page: parsePage(params.get('page'), AUDIT_MAX_PAGES),
    search: normalizeSearch(params.get('q'))
  };
}

export async function loadAuditPage(query: AuditQuery): Promise<AuditPageResult> {
  try {
    return await auditPage(query.page, query.search);
  } catch (error) {
    return {
      entries: [],
      page: query.page,
      page_size: AUDIT_PAGE_SIZE,
      max_pages: AUDIT_MAX_PAGES,
      has_more: false,
      error: error instanceof Error ? error.message : String(error)
    };
  }
}
