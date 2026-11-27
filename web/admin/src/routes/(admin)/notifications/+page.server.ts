// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import {
  notificationsList,
  notificationSend,
  notificationDelete,
  NOTIFICATIONS_MAX_PAGES
} from '#lib/server/services';
import { requireRole, type AdminIdentity } from '#lib/server/access';
import {
  audited,
  actionError,
  adminText,
  badRequest,
  okReply,
  refusalReply,
  type ParseResult
} from '#lib/server/admin-action';
import { parsePage } from '#lib/server/paging';
import {
  LEVELS,
  MAX_TITLE_LENGTH,
  MAX_BODY_LENGTH,
  type HistoryResult
} from '#lib/components/notifications/notification-compose';

async function loadHistory(page: number): Promise<HistoryResult> {
  try {
    const result = await notificationsList(page);
    return {
      notifications: result.notifications,
      page: result.page,
      hasMore: result.has_more,
      degraded: false
    };
  } catch {
    return {
      notifications: [],
      page,
      hasMore: false,
      degraded: true
    };
  }
}

export const load: PageServerLoad = ({ url }) => {
  const page = parsePage(url.searchParams.get('page'), NOTIFICATIONS_MAX_PAGES);
  return { page, history: loadHistory(page) };
};

type SendForm = {
  scope: 'broadcast' | 'direct';
  targetUserId: string;
  targetUsername: string;
  title: string;
  body: string;
  level: string;
  expiresAt?: string;
  target: string;
};

type SendFields = Omit<SendForm, 'scope' | 'target' | 'expiresAt'> & {
  scope: string;
  expiresAtRaw: string;
};

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
  { bad: (v) => !LEVELS.some((level) => level === v.level), error: 'invalid level' }
];

function parseSendForm(f: FormData, locale: App.Locals['locale']): ParseResult<SendForm> {
  const fields = sendFields(f);
  const error = SEND_RULES.find((rule) => rule.bad(fields))?.error;
  if (error) return badRequest(error);

  const expiresAt = Date.parse(fields.expiresAtRaw);
  if (fields.expiresAtRaw && !Number.isFinite(expiresAt)) {
    return badRequest(adminText(locale, 'admin.notifications.invalidExpiry'));
  }

  const scope = fields.scope as SendForm['scope'];
  return {
    value: {
      ...fields,
      scope,
      expiresAt: fields.expiresAtRaw ? new Date(expiresAt).toISOString() : undefined,
      target: scope === 'direct' ? fields.targetUserId || fields.targetUsername : 'all users'
    }
  };
}

function parseDeleteId(f: FormData): ParseResult<number> {
  const id = Number(String(f.get('id') ?? ''));
  if (!Number.isSafeInteger(id) || id <= 0) return badRequest('id required');
  return { value: id };
}

function sendPayload(v: SendForm, admin: AdminIdentity) {
  const direct = v.scope === 'direct';
  return {
    scope: v.scope,
    targetUserId: direct ? v.targetUserId : undefined,
    targetUsername: direct ? v.targetUsername : undefined,
    title: v.title,
    body: v.body,
    level: v.level,
    expiresAt: v.expiresAt,
    actorId: admin.id,
    actorLogin: admin.login
  };
}

type NotifActionSpec<P> = {
  name: string;
  parse: (f: FormData, locale: App.Locals['locale']) => ParseResult<P>;
  target: (payload: P) => string;
  detail?: (payload: P) => string;
  notice: (payload: P, locale: App.Locals['locale']) => string;
  run: (payload: P, admin: AdminIdentity) => Promise<unknown>;
};

function notifAction<P>(spec: NotifActionSpec<P>) {
  return async ({ request, locals }: { request: Request; locals: App.Locals }) => {
    const admin = await requireRole({ locals }, 'notifications.send');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });

    const parsed = spec.parse(await request.formData(), locals.locale);
    if ('refuse' in parsed) return refusalReply(parsed, locals.locale);
    const payload = parsed.value;

    return audited(
      { admin, action: spec.name, target: spec.target(payload), detail: spec.detail?.(payload) },
      () => spec.run(payload, admin),
      () => okReply(spec.notice(payload, locals.locale))
    );
  };
}

function recipientLabel(v: SendForm, locale: App.Locals['locale']): string {
  return v.scope === 'broadcast' ? adminText(locale, 'admin.notifications.audienceAll') : v.target;
}

export const actions: Actions = {
  send: notifAction<SendForm>({
    name: 'send_notification',
    parse: parseSendForm,
    target: (v) => v.target,
    detail: (v) => v.title,
    notice: (v, locale) => adminText(locale, 'admin.notifications.sentToTarget', { target: recipientLabel(v, locale) }),
    run: (v, admin) => notificationSend(sendPayload(v, admin))
  }),

  delete: notifAction<number>({
    name: 'delete_notification',
    parse: parseDeleteId,
    target: (id) => String(id),
    notice: (_id, locale) => adminText(locale, 'admin.notifications.retracted'),
    run: (id) => notificationDelete(id)
  })
};
