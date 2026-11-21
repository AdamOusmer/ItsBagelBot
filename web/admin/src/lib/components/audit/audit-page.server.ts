// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { normalizeSearch, parsePage } from '#lib/server/paging';
import {
  auditPage,
  AUDIT_MAX_PAGES,
  AUDIT_PAGE_SIZE,
  type AuditEntry,
  type AuditPage
} from '#lib/server/services';

type AuditQuery = { page: number; search: string };
export type AuditPageResult = AuditPage & { error?: string };

const DEMO = dev && process.env.DEMO === '1';

export function auditQuery(params: URLSearchParams): AuditQuery {
  return {
    page: parsePage(params.get('page'), AUDIT_MAX_PAGES),
    search: normalizeSearch(params.get('q'))
  };
}

function matchesSearch(entry: AuditEntry, q: string): boolean {
  const fields = [entry.actor_login, entry.action, String(entry.actor_id), entry.target, entry.detail, entry.error];
  return fields.some((field) => field?.toLowerCase().includes(q));
}

function demoPage({ page, search }: AuditQuery, entries: AuditEntry[]): AuditPage {
  const q = search.toLowerCase();
  const filtered = q ? entries.filter((entry) => matchesSearch(entry, q)) : entries;
  const start = (page - 1) * AUDIT_PAGE_SIZE;
  const end = start + AUDIT_PAGE_SIZE;
  return {
    entries: filtered.slice(start, end),
    page,
    page_size: AUDIT_PAGE_SIZE,
    max_pages: AUDIT_MAX_PAGES,
    has_more: end < Math.min(filtered.length, AUDIT_PAGE_SIZE * AUDIT_MAX_PAGES)
  };
}

export async function loadAuditPage(query: AuditQuery): Promise<AuditPageResult> {
  try {
    if (!DEMO) return await auditPage(query.page, query.search);
    const { demoAuditPageEntries } = await import('#lib/server/demo-data');
    return demoPage(query, demoAuditPageEntries());
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
