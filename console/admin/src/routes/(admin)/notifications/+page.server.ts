// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import { dev } from '$app/environment';
import {
  notificationsList,
  notificationSend,
  notificationDelete,
  auditAppend,
  NOTIFICATIONS_PAGE_SIZE,
  NOTIFICATIONS_MAX_PAGES,
  type NotificationWire
} from '$lib/server/services';
import { requireAdmin, type AdminIdentity } from '$lib/server/access';
import { parsePage } from '$lib/server/paging';

const LEVELS = new Set(['info', 'success', 'warning', 'critical']);
const MAX_TITLE_LENGTH = 120;
const MAX_BODY_LENGTH = 2000;
const DEMO = dev && process.env.DEMO === '1';

export type HistoryBundle = {
  notifications: NotificationWire[];
  page: number;
  pageSize: number;
  maxPages: number;
  hasMore: boolean;
  degraded: boolean;
};

function demoPage(page: number, notifications: NotificationWire[]): HistoryBundle {
  return {
    notifications,
    page,
    pageSize: NOTIFICATIONS_PAGE_SIZE,
    maxPages: NOTIFICATIONS_MAX_PAGES,
    hasMore: false,
    degraded: false
  };
}

async function loadHistory(page: number): Promise<HistoryBundle> {
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
      notifications: [],
      page,
      pageSize: NOTIFICATIONS_PAGE_SIZE,
      maxPages: NOTIFICATIONS_MAX_PAGES,
      hasMore: false,
      degraded: true
    };
  }
}

// Streamed: compose renders immediately; the sent history hydrates when the
// notifications RPC lands.
export const load: PageServerLoad = ({ url }) => {
  const page = parsePage(url.searchParams.get('page'), NOTIFICATIONS_MAX_PAGES);
  const history: Promise<HistoryBundle> = DEMO
    ? import('$lib/server/demo-data').then(({ sampleNotifications }) =>
        demoPage(page, sampleNotifications)
      )
    : loadHistory(page);
  return { history };
};

type SendForm = {
  scope: 'broadcast' | 'direct';
  targetUserId: string;
  targetUsername: string;
  title: string;
  body: string;
  level: string;
  expiresAtRaw: string;
  target: string;
};

// The compose fields as posted: trimmed, capped, not yet judged. Reading and
// validating are split so neither half carries the other's branches.
type SendFields = Omit<SendForm, 'scope' | 'target'> & { scope: string };

function sendFields(f: FormData): SendFields {
  return {
    scope: String(f.get('scope') ?? '').trim(),
    targetUserId: String(f.get('target_user_id') ?? '').trim(),
    targetUsername: String(f.get('target_username') ?? '').trim(),
    title: String(f.get('title') ?? '')
      .trim()
      .slice(0, MAX_TITLE_LENGTH),
    body: String(f.get('body') ?? '')
      .trim()
      .slice(0, MAX_BODY_LENGTH),
    level: String(f.get('level') ?? 'info').trim(),
    expiresAtRaw: String(f.get('expires_at') ?? '').trim()
  };
}

// A direct notification needs somebody to send it to; either identifier will
// do, and the send path resolves the username when only that is given.
function missingDirectTarget(v: SendFields): boolean {
  return v.scope === 'direct' && !v.targetUserId && !v.targetUsername;
}

// The first thing wrong with the form, or '' if nothing is.
function sendFormError(v: SendFields): string {
  if (v.scope !== 'broadcast' && v.scope !== 'direct') return 'invalid scope';
  if (missingDirectTarget(v)) return 'target user id or username required';
  if (!v.title || !v.body) return 'title and body are required';
  if (!LEVELS.has(v.level)) return 'invalid level';
  return '';
}

// parseSendForm trims/caps the compose fields and validates them. Returns the
// parsed form, or { error } for the action to hand to fail(400).
function parseSendForm(f: FormData): SendForm | { error: string } {
  const v = sendFields(f);
  const error = sendFormError(v);
  if (error) return { error };
  const scope = v.scope as SendForm['scope'];
  return {
    ...v,
    scope,
    target: scope === 'direct' ? v.targetUserId || v.targetUsername : 'all users'
  };
}

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
  if (DEMO) return;
  auditAppend({ actor_id: admin.id, actor_login: admin.login, action, target, detail, ok, error }).catch(
    () => {}
  );
}

export const actions: Actions = {
  send: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });

    const parsed = parseSendForm(await request.formData());
    if ('error' in parsed) return fail(400, { error: parsed.error });
    const { scope, targetUserId, targetUsername, title, body, level, expiresAtRaw, target } = parsed;

    if (DEMO) {
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

    if (DEMO) return { action: { ok: true, notice: 'notification retracted (demo)' } };

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
