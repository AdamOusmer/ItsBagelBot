import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { isManager, isDemo } from '$lib/server/access';
import { auditList, type AuditEntry } from '$lib/server/rpc';

const now = Date.now();
const sampleAudit: AuditEntry[] = [
  { id: 3, actor_id: 804932984, actor_login: 'itsmavey', action: 'set_status', target: '111111111', detail: 'paid', ok: true, created_at: new Date(now - 60_000).toISOString() },
  { id: 2, actor_id: 804932984, actor_login: 'itsmavey', action: 'staff_upsert', target: '222222222', detail: 'a_mod:moderator', ok: true, created_at: new Date(now - 3_600_000).toISOString() },
  { id: 1, actor_id: 804932984, actor_login: 'itsmavey', action: 'delete', target: '333333333', detail: '', ok: false, error: 'user not found', created_at: new Date(now - 7_200_000).toISOString() }
];

export const load: PageServerLoad = async ({ parent }) => {
  const admin = await parent();
  // The audit trail is sensitive (who did what); managers only.
  if (!isManager(admin.role)) throw redirect(302, '/');

  if (isDemo()) {
    return { entries: sampleAudit, degraded: false };
  }
  let entries: AuditEntry[] = [];
  let degraded = false;
  try {
    entries = await auditList(100);
  } catch {
    degraded = true;
  }
  return { entries, degraded };
};
