import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireAdmin, isManager } from '$lib/server/access';
import { auditPage, AUDIT_MAX_PAGES, AUDIT_PAGE_SIZE } from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

// Lazy per-member history. The staff drawer fetches this on open so the roster
// page never ships the whole audit log (keeps payload + render cheap).
export const GET: RequestHandler = async ({ url, locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin || !isManager(admin.role)) throw error(403, 'forbidden');

  const actorId = (url.searchParams.get('actor_id') ?? '').trim();
  if (!/^[0-9]+$/.test(actorId)) throw error(400, 'actor_id required');

  const pageStr = url.searchParams.get('page') ?? '1';
  let page = Number(pageStr);
  if (!Number.isFinite(page)) page = 1;
  page = Math.min(Math.max(Math.trunc(page), 1), AUDIT_MAX_PAGES);

  if (DEMO) {
    const { demoStaffHistory } = await import('$lib/server/demo-data');
    // In demo mode, only return data for page 1
    const entries = page === 1 ? demoStaffHistory(Number(actorId)) : [];
    return json({ entries, page, page_size: AUDIT_PAGE_SIZE, max_pages: AUDIT_MAX_PAGES, has_more: false });
  }

  try {
    const audit = await auditPage(page, '', actorId);
    return json(audit);
  } catch (e) {
    return json({ entries: [], page, page_size: AUDIT_PAGE_SIZE, max_pages: AUDIT_MAX_PAGES, has_more: false, error: (e as Error).message });
  }
};
