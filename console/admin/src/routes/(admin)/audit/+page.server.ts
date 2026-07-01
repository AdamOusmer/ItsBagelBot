import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { isManager } from '$lib/server/access';
import { AUDIT_MAX_PAGES, AUDIT_PAGE_SIZE } from '$lib/server/services';

const MAX_SEARCH_LENGTH = 200;

function parsePage(raw: string | null): number {
  const page = Number(raw ?? '1');
  if (!Number.isFinite(page)) return 1;
  return Math.min(Math.max(Math.trunc(page), 1), AUDIT_MAX_PAGES);
}

function normalizeSearch(raw: string | null): string {
  return (raw ?? '').trim().slice(0, MAX_SEARCH_LENGTH);
}

export const load: PageServerLoad = async ({ parent, url }) => {
  const admin = await parent();
  // The audit trail is sensitive (who did what); managers only.
  if (!isManager(admin.role)) throw redirect(302, '/');

  const page = parsePage(url.searchParams.get('page'));
  const search = normalizeSearch(url.searchParams.get('q'));

  return {
    page,
    pageSize: AUDIT_PAGE_SIZE,
    maxPages: AUDIT_MAX_PAGES,
    search
  };
};
