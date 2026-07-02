import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import {
  notificationsList,
  notificationSend,
  notificationDelete,
  auditAppend,
  NOTIFICATIONS_PAGE_SIZE,
  NOTIFICATIONS_MAX_PAGES,
  type NotificationWire
} from '$lib/server/services';
import { requireAdmin, isDemo, type AdminIdentity } from '$lib/server/access';
import { sampleNotifications } from '$lib/server/sample';

const LEVELS = new Set(['info', 'success', 'warning', 'critical']);
const MAX_TITLE_LENGTH = 120;
const MAX_BODY_LENGTH = 2000;

function parsePage(raw: string | null): number {
  const page = Number(raw ?? '1');
  if (!Number.isFinite(page)) return 1;
  return Math.min(Math.max(Math.trunc(page), 1), NOTIFICATIONS_MAX_PAGES);
}

function demoPage(page: number) {
  return {
    notifications: sampleNotifications,
    page,
    pageSize: NOTIFICATIONS_PAGE_SIZE,
    maxPages: NOTIFICATIONS_MAX_PAGES,
    hasMore: false,
    degraded: false
  };
}

export const load: PageServerLoad = async ({ url }) => {
  const page = parsePage(url.searchParams.get('page'));

  if (isDemo()) return demoPage(page);

  try {
    const result = await notificationsList(page);
    return {
      notifications: result.notifications,
      page: result.page,
      pageSize: result.page_size,
      maxPages: result.max_pages,
      hasMore: result.has_more,
      degraded: false
    };
  } catch {
    return {
      notifications: [] as NotificationWire[],
      page,
      pageSize: NOTIFICATIONS_PAGE_SIZE,
      maxPages: NOTIFICATIONS_MAX_PAGES,
      hasMore: false,
      degraded: true
    };
  }
};

// audit records a mutating action best-effort: a logging failure must never
// block or fail the operator action it describes. Skipped in demo (synthetic
// non-numeric actor id).
function audit(
  admin: AdminIdentity,
  action: string,
  target: string,
  detail: string,
  ok: boolean,
  error?: string
): void {
  if (isDemo()) return;
  auditAppend({ actor_id: admin.id, actor_login: admin.login, action, target, detail, ok, error }).catch(
    () => {}
  );
}

export const actions: Actions = {
  send: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });

    const f = await request.formData();
    const scope = String(f.get('scope') ?? '').trim();
    const targetUserId = String(f.get('target_user_id') ?? '').trim();
    const targetUsername = String(f.get('target_username') ?? '').trim();
    const title = String(f.get('title') ?? '').trim().slice(0, MAX_TITLE_LENGTH);
    const body = String(f.get('body') ?? '').trim().slice(0, MAX_BODY_LENGTH);
    const level = String(f.get('level') ?? 'info').trim();
    const expiresAtRaw = String(f.get('expires_at') ?? '').trim();

    if (scope !== 'broadcast' && scope !== 'direct') return fail(400, { error: 'invalid scope' });
    if (scope === 'direct' && !targetUserId && !targetUsername) {
      return fail(400, { error: 'target user id or username required' });
    }
    if (!title || !body) return fail(400, { error: 'title and body are required' });
    if (!LEVELS.has(level)) return fail(400, { error: 'invalid level' });

    const target = scope === 'direct' ? targetUserId || targetUsername : 'all users';

    if (isDemo()) {
      return { action: { ok: true, notice: `notification sent to ${target} (demo)` } };
    }

    try {
      await notificationSend({
        scope,
        targetUserId: scope === 'direct' ? targetUserId : undefined,
        targetUsername: scope === 'direct' ? targetUsername : undefined,
        title,
        body,
        level,
        expiresAt: expiresAtRaw ? new Date(expiresAtRaw).toISOString() : undefined,
        actorId: admin.id,
        actorLogin: admin.login
      });
      audit(admin, 'send_notification', target, title, true);
      return { action: { ok: true, notice: `notification sent to ${target}` } };
    } catch (e) {
      audit(admin, 'send_notification', target, title, false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  delete: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const id = Number(String((await request.formData()).get('id') ?? ''));
    if (!Number.isFinite(id) || id <= 0) return fail(400, { error: 'id required' });

    if (isDemo()) return { action: { ok: true, notice: 'notification retracted (demo)' } };

    try {
      await notificationDelete(id);
      audit(admin, 'delete_notification', String(id), '', true);
      return { action: { ok: true, notice: 'notification retracted' } };
    } catch (e) {
      audit(admin, 'delete_notification', String(id), '', false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  }
};
