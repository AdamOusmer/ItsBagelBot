import type { RequestHandler } from './$types';
import { json, error } from '@sveltejs/kit';
import { requireAdmin, isManager, isDemo } from '$lib/server/access';
import { auditList, type AuditEntry } from '$lib/server/rpc';

// Lazy per-member history. The staff drawer fetches this on open so the roster
// page never ships the whole audit log (keeps payload + render cheap).
export const GET: RequestHandler = async ({ url, locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin || !isManager(admin.role)) throw error(403, 'forbidden');

  const actorId = (url.searchParams.get('actor_id') ?? '').trim();
  if (!/^[0-9]+$/.test(actorId)) throw error(400, 'actor_id required');

  if (isDemo()) {
    const now = Date.now();
    const sample: AuditEntry[] = [
      { id: 2, actor_id: Number(actorId), actor_login: 'demo', action: 'set_status', target: '111111111', detail: 'paid', ok: true, created_at: new Date(now - 3_600_000).toISOString() },
      { id: 1, actor_id: Number(actorId), actor_login: 'demo', action: 'staff_upsert', target: '222222222', detail: 'a_mod:moderator', ok: true, created_at: new Date(now - 7_200_000).toISOString() }
    ];
    return json({ entries: sample });
  }

  try {
    const entries = await auditList(50, actorId);
    return json({ entries });
  } catch (e) {
    return json({ entries: [], error: (e as Error).message });
  }
};
