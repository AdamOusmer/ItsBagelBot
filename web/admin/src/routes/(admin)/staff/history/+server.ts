// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { dev } from '$app/env';
import { requireRole } from '#lib/server/access';
import { parsePage } from '#lib/server/paging';
import { auditPage, AUDIT_MAX_PAGES, AUDIT_PAGE_SIZE } from '#lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

export const GET: RequestHandler = async ({ url, locals }) => {
  const admin = await requireRole({ locals }, 'staff.manage');
  if (!admin) throw error(403, 'forbidden');

  const actorId = (url.searchParams.get('actor_id') ?? '').trim();
  if (!/^[0-9]+$/.test(actorId)) throw error(400, 'actor_id required');

  const page = parsePage(url.searchParams.get('page'), AUDIT_MAX_PAGES);

  const pagination = { page, page_size: AUDIT_PAGE_SIZE, max_pages: AUDIT_MAX_PAGES, has_more: false };
  const read = DEMO
    ? import('#lib/server/demo-data').then(({ demoStaffHistory }) => ({
        ...pagination,
        entries: page === 1 ? demoStaffHistory(Number(actorId)) : []
      }))
    : auditPage(page, '', actorId);
  const audit = await read.catch(() => ({
    ...pagination,
    entries: [],
    error: 'staff_history_unavailable'
  }));
  const entries = audit.entries.map((entry) => entry.action.startsWith('db_credential_')
    ? { ...entry, error: undefined }
    : entry);
  return json({ ...audit, entries });
};
