// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import { dev } from '$app/environment';
import {
  notificationsList,
  notificationSend,
  notificationDelete,
  NOTIFICATIONS_PAGE_SIZE,
  NOTIFICATIONS_MAX_PAGES,
  type NotificationWire
} from '$lib/server/services';
import { requireRole, type AdminIdentity } from '$lib/server/access';
import {
  audited,
  actionError,
  adminText,
  badRequest,
  okReply,
  refusalReply,
  type ParseResult
} from '$lib/server/admin-action';
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

function missingDirectTarget(v: SendFields): boolean {
  return v.scope === 'direct' && !v.targetUserId && !v.targetUsername;
}

const SEND_RULES: { bad: (v: SendFields) => boolean; error: string }[] = [
  { bad: (v) => v.scope !== 'broadcast' && v.scope !== 'direct', error: 'invalid scope' },
  { bad: missingDirectTarget, error: 'target user id or username required' },
  { bad: (v) => !v.title || !v.body, error: 'title and body are required' },
  { bad: (v) => !LEVELS.has(v.level), error: 'invalid level' }
];

function sendFormError(v: SendFields): string {
  return SEND_RULES.find((rule) => rule.bad(v))?.error ?? '';
}

function parseSendForm(f: FormData): ParseResult<SendForm> {
  const v = sendFields(f);
  const error = sendFormError(v);
  if (error) return badRequest(error);
  const scope = v.scope as SendForm['scope'];
  return {
    value: {
      ...v,
      scope,
      target: scope === 'direct' ? v.targetUserId || v.targetUsername : 'all users'
    }
  };
}

function parseDeleteId(f: FormData): ParseResult<number> {
  const id = Number(String(f.get('id') ?? ''));
  if (!Number.isFinite(id) || id <= 0) return badRequest('id required');
  return { value: id };
}

// Drop targets on a broadcast: a present-but-blank target_user_id silently sends to nobody.
function sendPayload(v: SendForm, admin: AdminIdentity) {
  const direct = v.scope === 'direct';
  return {
    scope: v.scope,
    targetUserId: direct ? v.targetUserId : undefined,
    targetUsername: direct ? v.targetUsername : undefined,
    title: v.title,
    body: v.body,
    level: v.level,
    expiresAt: v.expiresAtRaw ? new Date(v.expiresAtRaw).toISOString() : undefined,
    actorId: admin.id,
    actorLogin: admin.login
  };
}

type NotifActionSpec<P> = {
  name: string;
  parse: (f: FormData) => ParseResult<P>;
  target: (payload: P) => string;
  detail?: (payload: P) => string;
  demoNotice: (payload: P, locale: App.Locals['locale']) => string;
  notice: (payload: P, locale: App.Locals['locale']) => string;
  run: (payload: P, admin: AdminIdentity) => Promise<unknown>;
};

function notifAction<P>(spec: NotifActionSpec<P>) {
  return async ({ request, locals }: { request: Request; locals: App.Locals }) => {
    const admin = await requireRole({ locals }, 'notifications.send');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });

    const parsed = spec.parse(await request.formData());
    if ('refuse' in parsed) return refusalReply(parsed, locals.locale);
    const payload = parsed.value;

    if (DEMO) return okReply(spec.demoNotice(payload, locals.locale));
    return audited(
      { admin, action: spec.name, target: spec.target(payload), detail: spec.detail?.(payload) },
      () => spec.run(payload, admin),
      () => okReply(spec.notice(payload, locals.locale))
    );
  };
}

export const actions: Actions = {
  send: notifAction<SendForm>({
    name: 'send_notification',
    parse: parseSendForm,
    target: (v) => v.target,
    detail: (v) => v.title,
    demoNotice: (v, locale) => adminText(locale, 'admin.notifications.sentDemo', { target: v.target }),
    notice: (v, locale) => adminText(locale, 'admin.notifications.sentToTarget', { target: v.target }),
    run: (v, admin) => notificationSend(sendPayload(v, admin))
  }),

  delete: notifAction<number>({
    name: 'delete_notification',
    parse: parseDeleteId,
    target: (id) => String(id),
    demoNotice: (_id, locale) => adminText(locale, 'admin.notifications.retractedDemo'),
    notice: (_id, locale) => adminText(locale, 'admin.notifications.retracted'),
    run: (id) => notificationDelete(id)
  })
};
