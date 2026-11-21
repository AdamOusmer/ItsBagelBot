// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import { fail } from '@sveltejs/kit';
import { dev } from '$app/env';
import {
  userLookup,
  userSetStatus,
  userSetActive,
  userSetTestAccount,
  userSetCreatorCode,
  userBan,
  userUnban,
  userReset,
  userDelete,
  tokenClear,
  tokenStatus,
  restartUserEventSub,
  publishUserEventSub,
  channelSubState,
  premiumGrantEmail,
  isForbidden,
  type AdminUserWire,
  type GrantEmailOutcome,
  type ChannelSubState,
  type UserRef
} from '#lib/server/services';
import { requireRole, type AccessKey, type AdminIdentity } from '#lib/server/access';
import {
  audited,
  actionError,
  adminText,
  badRequest,
  okReply,
  refusalReply,
  refused,
  softNotice,
  type ParseRefusal,
  type ParseResult
} from '#lib/server/admin-action';
import { audit } from '#lib/server/audit';
import { signViewAs } from '@bagel/kit/server/impersonation';
import { env } from '@bagel/kit/server/env';
import { directoryQuery, loadUserDirectory } from '#lib/server/user-directory';

const CREATOR_CODE_MAX_LENGTH = 64;
const LOOKUP_MAX_LENGTH = 128;
const DEMO = dev && process.env.DEMO === '1';
const STATUSES = new Set(['free', 'paid', 'vip']);
const UNKNOWN_SUB_STATE: ChannelSubState = { state: 'unknown', error: '', checkedAt: null };
const DEMO_SUB_STATE: ChannelSubState = { state: 'ok', error: '', checkedAt: null };
type Locale = App.Locals['locale'];

export const load: PageServerLoad = async ({ url, parent }) => {
  const { id } = await parent();
  const query = directoryQuery(url);
  return { directory: loadUserDirectory(id, query), ...query };
};

function dashboardOrigin(url: URL): string {
  const configured = (env.DASHBOARD_PUBLIC_ORIGIN ?? '').trim().replace(/\/+$/, '');
  if (configured) return configured;
  if (dev) return url.origin;
  throw new Error('DASHBOARD_PUBLIC_ORIGIN not set');
}

function demoLookup(query: LookupQuery, sampleUsers: AdminUserWire[]) {
  const user = sampleUsers.find((sample) => sample.username === query.q || String(sample.id) === query.q);
  if (!user) return { lookup: { error: adminText(query.locale, 'admin.users.notFound'), q: query.q } };
  return {
    lookup: {
      user,
      tokenPresent: user.status !== 'free',
      subState: DEMO_SUB_STATE
    }
  };
}

type LookupQuery = {
  actorId: UserRef['actorId'];
  q: string;
  locale: Locale;
};

async function probeUser({ actorId, q }: LookupQuery) {
  const user = await userLookup(actorId, q);
  const userId = String(user.id);
  const [token, subscription] = await Promise.allSettled([
    tokenStatus({ actorId, userId }),
    channelSubState(userId, { verify: true })
  ]);
  return {
    user,
    tokenPresent: token.status === 'fulfilled' ? token.value.present : false,
    subState: subscription.status === 'fulfilled' ? subscription.value : UNKNOWN_SUB_STATE
  };
}

type UserTarget = {
  admin: AdminIdentity;
  ref: UserRef;
  locale: Locale;
};

type UserMutation<P> = {
  name: string;
  key: AccessKey;
  parse: (form: FormData) => ParseResult<P>;
  demo?: (target: UserTarget, input: P) => unknown;
  notice: (user: AdminUserWire | null, input: P, locale: Locale) => string;
  detail?: (input: P) => string;
  run: (ref: UserRef, input: P) => Promise<AdminUserWire | null>;
};

function formField(form: FormData, name: string): string {
  return String(form.get(name) ?? '').trim();
}

function withUserTarget<R>(
  key: AccessKey,
  run: (target: UserTarget, form: FormData, url: URL) => R
) {
  return async ({ request, locals, url }: RequestEvent) => {
    const { locale } = locals;
    const admin = await requireRole({ locals }, key);
    if (!admin) return fail(403, { error: actionError(locale, 'forbidden') });

    const form = await request.formData();
    const userId = formField(form, 'user_id');
    if (!userId) return fail(400, { error: actionError(locale, 'user_id required') });
    return run({ admin, ref: { actorId: admin.id, userId }, locale }, form, url);
  };
}

function userReply(notice: string, user: AdminUserWire | null) {
  const reply = okReply(notice);
  return user ? { ...reply, lookup: { user } } : reply;
}

function userAction<P>(mutation: UserMutation<P>) {
  return withUserTarget(mutation.key, (target, form) => {
    const { admin, ref, locale } = target;
    const input = mutation.parse(form);
    if ('refuse' in input) return refusalReply(input, locale);
    if (DEMO && mutation.demo) return mutation.demo(target, input.value);
    return audited(
      { admin, action: mutation.name, target: ref.userId, detail: mutation.detail?.(input.value) },
      () => mutation.run(ref, input.value),
      (user) => userReply(mutation.notice(user, input.value, locale), user)
    );
  });
}

type FixedNoticeMutation = Pick<UserMutation<null>, 'name' | 'key' | 'run'> & {
  notice: string;
  demoNotice: string;
};

function fixedNoticeAction({ name, key, run, notice, demoNotice }: FixedNoticeMutation) {
  return userAction({
    name,
    key,
    run,
    parse: () => ({ value: null }),
    demo: ({ locale }) => okReply(adminText(locale, demoNotice)),
    notice: (_user, _input, locale) => adminText(locale, notice)
  });
}

function parsePaidGrant(form: FormData): { expiresAt: string; detail: string } | ParseRefusal {
  const raw = formField(form, 'expires_at');
  if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) {
    return softNotice('paid grant needs an end date');
  }
  const end = new Date(`${raw}T23:59:59.999Z`);
  if (Number.isNaN(end.getTime()) || end.getTime() <= Date.now()) {
    return softNotice('end date must be in the future');
  }
  if (end.getTime() > Date.now() + 5 * 365 * 864e5) {
    return softNotice('end date is too far out (max 5 years)');
  }
  const start = new Date().toISOString().slice(0, 10);
  return { expiresAt: end.toISOString(), detail: `status=paid start=${start} end=${raw}` };
}

type GrantEmailResult = GrantEmailOutcome | 'failed';

type StatusGrant = { status: string; expiresAt?: string; detail: string; notify: boolean; email?: GrantEmailResult };

const GRANT_EMAIL_NOTICE: Record<GrantEmailResult, string> = {
  sent: 'admin.users.grantEmail.sent',
  no_email: 'admin.users.grantEmail.noAddress',
  failed: 'admin.users.grantEmail.failed'
};

function parseStatus(form: FormData): ParseResult<StatusGrant> {
  const status = formField(form, 'status');
  if (!STATUSES.has(status)) return badRequest('invalid status');
  if (status !== 'paid') return { value: { status, detail: `status=${status}`, notify: false } };
  const grant = parsePaidGrant(form);
  if ('refuse' in grant) return grant;
  const notify = form.get('notify_email') === 'on';
  const detail = notify ? `${grant.detail} email=requested` : grant.detail;
  return { value: { status, expiresAt: grant.expiresAt, detail, notify } };
}

async function grantEmail(ref: UserRef, user: AdminUserWire | null): Promise<GrantEmailResult | undefined> {
  if (!user?.subscription_expires_at) return undefined;
  try {
    return await premiumGrantEmail(ref, user.subscription_expires_at);
  } catch {
    return 'failed';
  }
}

async function applyStatus(ref: UserRef, grant: StatusGrant): Promise<AdminUserWire | null> {
  const user = await userSetStatus(ref, grant.status, grant.expiresAt);
  if (grant.notify) grant.email = await grantEmail(ref, user);
  return user;
}

function statusNotice(user: AdminUserWire | null, grant: StatusGrant, locale: Locale): string {
  if (!user) return adminText(locale, 'admin.users.statusSet', { status: grant.status });
  const until = user.subscription_expires_at
    ? ` until ${user.subscription_expires_at.slice(0, 10)}`
    : '';
  const notice = adminText(locale, 'admin.users.statusSetUntil', { status: user.status, until });
  return grant.email ? `${notice} · ${adminText(locale, GRANT_EMAIL_NOTICE[grant.email])}` : notice;
}

type CreatorCode = { code: string; detail: string };

function parseCreatorCode(form: FormData): ParseResult<CreatorCode> {
  const code = formField(form, 'creator_code');
  if (code.length > CREATOR_CODE_MAX_LENGTH) {
    return softNotice(`creator code must be ${CREATOR_CODE_MAX_LENGTH} characters or fewer`);
  }
  return { value: { code, detail: code ? `creator_code=${code}` : 'creator_code=cleared' } };
}

function creatorCodeNotice(user: AdminUserWire | null, _input: CreatorCode, locale: Locale): string {
  return user?.creator_code
    ? adminText(locale, 'admin.users.creatorCodeSet', { code: user.creator_code })
    : adminText(locale, 'admin.users.creatorCodeCleared');
}

async function demoCreatorCode({ ref, locale }: UserTarget, { code }: CreatorCode) {
  const { sampleUsers } = await import('#lib/server/demo-data');
  const user = sampleUsers.find((sample) => String(sample.id) === ref.userId);
  const notice = code
    ? adminText(locale, 'admin.users.creatorCodeSetDemo', { code })
    : adminText(locale, 'admin.users.creatorCodeClearedDemo');
  return { ...okReply(notice), lookup: user ? { user: { ...user, creator_code: code || null } } : undefined };
}

function parseActive(form: FormData): ParseResult<boolean> {
  return { value: formField(form, 'active') === 'true' };
}

function needsEnrollment(user: AdminUserWire | null): boolean {
  return user !== null && user.is_active && !user.banned;
}

async function withEnrollmentSync<T extends AdminUserWire | null>(
  userId: UserRef['userId'],
  sync: 'unenroll-first' | 'enroll-after',
  mutate: () => Promise<T>
): Promise<T> {
  if (sync === 'unenroll-first') {
    await publishUserEventSub(userId, false);
    return mutate();
  }
  const user = await mutate();
  if (needsEnrollment(user)) await publishUserEventSub(userId, true);
  return user;
}

async function lookupUser({ request, locals }: RequestEvent) {
  const { locale } = locals;
  const admin = await requireRole({ locals }, 'users.read');
  if (!admin) return fail(403, { error: actionError(locale, 'forbidden') });
  const q = formField(await request.formData(), 'q');
  if (!q) return fail(400, { error: actionError(locale, 'query required') });
  if (q.length > LOOKUP_MAX_LENGTH) return fail(400, { error: actionError(locale, 'query too long') });
  const query: LookupQuery = { actorId: admin.id, q, locale };
  if (DEMO) {
    const { sampleUsers } = await import('#lib/server/demo-data');
    return demoLookup(query, sampleUsers);
  }
  try {
    return { lookup: await probeUser(query) };
  } catch (error) {
    if (isForbidden(error)) return refused(error);
    return { lookup: { error: (error as Error).message, q } };
  }
}

function actionNotice(error: unknown) {
  return { action: { ok: false, notice: (error as Error).message } };
}

async function restartForUser({ admin, ref, locale }: UserTarget) {
  if (DEMO) {
    return { ...okReply(adminText(locale, 'admin.users.restartDemo')), subState: DEMO_SUB_STATE };
  }
  try {
    await restartUserEventSub(ref.userId);
    audit(admin, { action: 'restart', target: ref.userId, ok: true });
    const subState = await channelSubState(ref.userId).catch(() => UNKNOWN_SUB_STATE);
    return { ...okReply(adminText(locale, 'admin.users.restartQueued')), subState };
  } catch (error) {
    audit(admin, { action: 'restart', target: ref.userId, ok: false, error: (error as Error).message });
    return actionNotice(error);
  }
}

type ViewAsSubject = { id: string; login: string; displayName: string };

function makeViewAsUrl(origin: string, admin: AdminIdentity, subject: ViewAsSubject): string {
  const token = signViewAs({
    sub: subject.id,
    login: subject.login,
    display_name: subject.displayName,
    by_id: admin.id,
    by_login: admin.login
  });
  return `${origin}/auth/impersonate?t=${token}`;
}

async function impersonateUser({ admin, ref, locale }: UserTarget, _: FormData, url: URL) {
  let origin: string;
  try {
    origin = dashboardOrigin(url);
  } catch (error) {
    return actionNotice(error);
  }
  if (DEMO) {
    const viewAsUrl = makeViewAsUrl(origin, admin, { id: ref.userId, login: 'demo', displayName: 'Demo' });
    return { ...okReply(adminText(locale, 'admin.users.viewAsDemo')), viewAsUrl };
  }
  try {
    const user = await userLookup(admin.id, ref.userId);
    const viewAsUrl = makeViewAsUrl(origin, admin, {
      id: String(user.id),
      login: user.username,
      displayName: user.username
    });
    audit(admin, { action: 'impersonate', target: ref.userId, ok: true });
    return { ...okReply(adminText(locale, 'admin.users.viewAs')), viewAsUrl };
  } catch (error) {
    audit(admin, { action: 'impersonate', target: ref.userId, ok: false, error: (error as Error).message });
    if (isForbidden(error)) return refused(error);
    return actionNotice(error);
  }
}

export const actions: Actions = {
  lookup: lookupUser,

  setStatus: userAction({
    name: 'set_status',
    key: 'users.grant',
    parse: parseStatus,
    demo: (_target, grant) => okReply(`status set to ${grant.status} (demo)`),
    detail: (grant) => grant.detail,
    notice: statusNotice,
    run: applyStatus
  }),

  reset: fixedNoticeAction({
    name: 'reset',
    key: 'users.grant',
    demoNotice: 'admin.users.userResetDemo',
    notice: 'admin.users.userReset',
    run: userReset
  }),

  clearToken: fixedNoticeAction({
    name: 'clear_token',
    key: 'users.token',
    demoNotice: 'admin.users.tokenClearedDemo',
    notice: 'admin.users.tokenCleared',
    run: async (ref) => {
      await tokenClear(ref);
      return null;
    }
  }),

  setActive: userAction({
    name: 'set_active',
    key: 'users.grant',
    parse: parseActive,
    demo: ({ locale }) => okReply(adminText(locale, 'admin.users.activeSetDemo')),
    notice: (user, _active, locale) => adminText(locale, 'admin.users.active', { active: user?.is_active ? 'true' : 'false' }),
    detail: String,
    run: (ref, active) =>
      withEnrollmentSync(ref.userId, active ? 'enroll-after' : 'unenroll-first', () => userSetActive(ref, active))
  }),

  setTestAccount: userAction({
    name: 'set_test_account',
    key: 'users.test',
    parse: parseActive,
    demo: ({ locale }, active) => okReply(adminText(locale, 'admin.users.testAccountDemo', { active: String(active) })),
    notice: (user, _active, locale) => adminText(locale, 'admin.users.testAccount', { active: user?.test_account ? 'on' : 'off' }),
    run: userSetTestAccount
  }),

  setCreatorCode: userAction({
    name: 'set_creator_code',
    key: 'users.grant',
    parse: parseCreatorCode,
    demo: DEMO ? demoCreatorCode : undefined,
    detail: (input) => input.detail,
    notice: creatorCodeNotice,
    run: (ref, input) => userSetCreatorCode(ref, input.code)
  }),

  ban: fixedNoticeAction({
    name: 'ban',
    key: 'users.ban',
    demoNotice: 'admin.users.userBannedDemo',
    notice: 'admin.users.userBanned',
    run: (ref) => withEnrollmentSync(ref.userId, 'unenroll-first', () => userBan(ref))
  }),

  unban: fixedNoticeAction({
    name: 'unban',
    key: 'users.ban',
    demoNotice: 'admin.users.userUnbannedDemo',
    notice: 'admin.users.userUnbanned',
    run: (ref) => withEnrollmentSync(ref.userId, 'enroll-after', () => userUnban(ref))
  }),

  restart: withUserTarget('users.restart', restartForUser),

  impersonate: withUserTarget('users.impersonate', impersonateUser),

  delete: fixedNoticeAction({
    name: 'delete',
    key: 'users.delete',
    demoNotice: 'admin.users.userDeletedDemo',
    notice: 'admin.users.userDeleted',
    run: (ref) => withEnrollmentSync(ref.userId, 'unenroll-first', async () => {
      await userDelete(ref);
      return null;
    })
  })
};
