import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireAdmin, isManager } from '$lib/server/access';
import {
  auditPage,
  AUDIT_MAX_PAGES,
  AUDIT_PAGE_SIZE,
  type AuditEntry
} from '$lib/server/services';

const MAX_SEARCH_LENGTH = 200;
const DEMO = dev && process.env.DEMO === '1';

function parsePage(raw: string | null): number {
  const page = Number(raw ?? '1');
  if (!Number.isFinite(page)) return 1;
  return Math.min(Math.max(Math.trunc(page), 1), AUDIT_MAX_PAGES);
}

function normalizeSearch(raw: string | null): string {
  return (raw ?? '').trim().slice(0, MAX_SEARCH_LENGTH);
}

function matchesSearch(entry: AuditEntry, search: string): boolean {
  if (!search) return true;
  const q = search.toLowerCase();
  return (
    entry.actor_login.toLowerCase().includes(q) ||
    entry.action.toLowerCase().includes(q) ||
    String(entry.actor_id).includes(q) ||
    (entry.target ?? '').toLowerCase().includes(q) ||
    (entry.detail ?? '').toLowerCase().includes(q) ||
    (entry.error ?? '').toLowerCase().includes(q)
  );
}

function demoPage(page: number, search: string, sampleAudit: AuditEntry[]) {
  const filtered = sampleAudit.filter((entry) => matchesSearch(entry, search));
  const start = (page - 1) * AUDIT_PAGE_SIZE;
  const entries = filtered.slice(start, start + AUDIT_PAGE_SIZE);
  const cappedTotal = Math.min(filtered.length, AUDIT_PAGE_SIZE * AUDIT_MAX_PAGES);
  return {
    entries,
    page,
    page_size: AUDIT_PAGE_SIZE,
    max_pages: AUDIT_MAX_PAGES,
    has_more: start + AUDIT_PAGE_SIZE < cappedTotal
  };
}

export const GET: RequestHandler = async ({ url, locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin || !isManager(admin.role)) throw error(403, 'forbidden');

  const page = parsePage(url.searchParams.get('page'));
  const search = normalizeSearch(url.searchParams.get('q'));

  if (DEMO) {
    const { demoAuditPageEntries } = await import('$lib/server/demo-data');
    return json(demoPage(page, search, demoAuditPageEntries()));
  }

  try {
    const audit = await auditPage(page, search);
    return json(audit);
  } catch (e) {
    return json({
      entries: [],
      page,
      page_size: AUDIT_PAGE_SIZE,
      max_pages: AUDIT_MAX_PAGES,
      has_more: false,
      error: (e as Error).message
    });
  }
};
