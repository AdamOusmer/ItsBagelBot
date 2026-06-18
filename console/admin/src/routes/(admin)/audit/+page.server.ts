import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { isManager, isDemo } from '$lib/server/access';
import {
  auditPage,
  AUDIT_MAX_PAGES,
  AUDIT_PAGE_SIZE,
  type AuditEntry
} from '$lib/server/rpc';

const MAX_SEARCH_LENGTH = 200;

const now = Date.now();
const sampleAudit: AuditEntry[] = [
  { id: 3, actor_id: 804932984, actor_login: 'itsmavey', action: 'set_status', target: '111111111', detail: 'paid', ok: true, created_at: new Date(now - 60_000).toISOString() },
  { id: 2, actor_id: 804932984, actor_login: 'itsmavey', action: 'staff_upsert', target: '222222222', detail: 'a_mod:moderator', ok: true, created_at: new Date(now - 3_600_000).toISOString() },
  { id: 1, actor_id: 804932984, actor_login: 'itsmavey', action: 'delete', target: '333333333', detail: '', ok: false, error: 'user not found', created_at: new Date(now - 7_200_000).toISOString() }
];

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

function demoPage(page: number, search: string) {
  const filtered = sampleAudit.filter((entry) => matchesSearch(entry, search));
  const start = (page - 1) * AUDIT_PAGE_SIZE;
  const entries = filtered.slice(start, start + AUDIT_PAGE_SIZE);
  const cappedTotal = Math.min(filtered.length, AUDIT_PAGE_SIZE * AUDIT_MAX_PAGES);
  return {
    entries,
    page,
    pageSize: AUDIT_PAGE_SIZE,
    maxPages: AUDIT_MAX_PAGES,
    hasMore: start + AUDIT_PAGE_SIZE < cappedTotal,
    search,
    degraded: false
  };
}

export const load: PageServerLoad = async ({ parent, url }) => {
  const admin = await parent();
  // The audit trail is sensitive (who did what); managers only.
  if (!isManager(admin.role)) throw redirect(302, '/');

  const page = parsePage(url.searchParams.get('page'));
  const search = normalizeSearch(url.searchParams.get('q'));

  if (isDemo()) {
    return demoPage(page, search);
  }
  let degraded = false;
  try {
    const audit = await auditPage(page, search);
    return {
      entries: audit.entries,
      page: audit.page,
      pageSize: audit.page_size,
      maxPages: audit.max_pages,
      hasMore: audit.has_more,
      search,
      degraded
    };
  } catch {
    degraded = true;
  }
  return {
    entries: [],
    page,
    pageSize: AUDIT_PAGE_SIZE,
    maxPages: AUDIT_MAX_PAGES,
    hasMore: false,
    search,
    degraded
  };
};
