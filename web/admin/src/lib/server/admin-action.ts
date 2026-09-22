// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The pieces every gated console mutation is assembled from.
//
// Each admin action runs the same spine: gate on a role, parse the posted
// form, short-circuit under DEMO, run one RPC, record the outcome, and turn a
// service's refusal into the right kind of reply. Written out per action, that
// spine made every pair of actions in a route file structurally identical --
// the shape CodeScene flags as sibling duplication, and the same reason
// audit.ts exists. The route-specific wrappers (userAction, notifAction) keep
// their own parse and reply shaping and share the parts below.
import { fail } from '@sveltejs/kit';
import { audit } from './audit';
import { isForbidden } from './services';
import type { AdminIdentity } from './access';
import { translate } from '@bagel/kit/i18n';
import type { Locale } from '@bagel/kit/i18n';

export function adminText(locale: Locale, key: string, params?: Record<string, string | number>): string {
  return translate(locale, key, params);
}

// Fixed action failures use explicit catalog keys. Unknown messages are
// service or validation details and stay verbatim for operators.
const FIXED_ACTION_KEYS: Record<string, string> = {
  forbidden: 'admin.action.forbidden',
  'numeric user id required': 'admin.action.numericUserIdRequired',
  'login required': 'admin.action.loginRequired',
  'invalid role': 'admin.action.invalidRole',
  'cannot remove this member': 'admin.action.cannotRemoveMember',
  'user_id required': 'admin.action.userIdRequired',
  'query required': 'admin.action.queryRequired',
  'query too long': 'admin.action.queryTooLong',
  'id required': 'admin.action.idRequired',
  'title required': 'admin.giveaway.titleRequired',
  'internal reason required': 'admin.giveaway.reasonRequired',
  'winner count must be a positive whole number': 'admin.giveaway.winnerCountPositive',
  'prize duration must be a whole number from 1 to 12': 'admin.giveaway.prizeMonthsRange',
  'giveaway id required': 'admin.giveaway.idRequired',
  'snapshot version and operation key required': 'admin.giveaway.operationRequired',
  'pool digest required': 'admin.giveaway.poolDigestRequired',
  'award id required': 'admin.giveaway.awardIdRequired',
  'invalid scope': 'admin.notifications.invalidScope',
  'target user id or username required': 'admin.notifications.targetRequired',
  'title and body are required': 'admin.notifications.titleBodyRequired',
  'invalid level': 'admin.notifications.invalidLevel'
};

export function actionError(locale: Locale, message: string): string {
  const key = FIXED_ACTION_KEYS[message];
  return key ? adminText(locale, key) : message;
}

// A parse step refuses by returning this in place of its payload.
//
// The two kinds are not interchangeable. 'bad-request' means the form could
// not have come from the console's own UI (a missing id, an unknown status)
// and becomes a 400. 'notice' is a correctable mistake the operator can see
// and fix (an end date in the past, an over-long creator code); it rides a 200
// so SvelteKit does not discard what they typed.
export type ParseRefusal = { refuse: 'bad-request' | 'notice'; message: string };

export type ParseResult<P> = { value: P } | ParseRefusal;

export function badRequest(message: string): ParseRefusal {
  return { refuse: 'bad-request', message };
}

export function softNotice(message: string): ParseRefusal {
  return { refuse: 'notice', message };
}

export function okReply(notice: string) {
  return { action: { ok: true, notice } };
}

export function refusalReply(r: ParseRefusal, locale?: Locale) {
  const message = locale ? actionError(locale, r.message) : r.message;
  if (r.refuse === 'bad-request') return fail(400, { error: message });
  return { action: { ok: false, notice: message } };
}

// refused turns a service's own role refusal into the same 403 the console's
// table produces. The two ladders agree today; if they ever drift, the
// operator must see the service's answer as a refusal, not as a retryable
// notice.
export function refused(e: unknown) {
  return fail(403, { error: (e as Error).message });
}

export type AuditRef = {
  admin: AdminIdentity;
  action: string; // audit action id
  target: string;
  detail?: string;
};

// audited runs one console mutation and records it either way, then hands the
// result to `shape` for the route's own reply. The audit line is written
// before the reply is built so a shaping bug cannot lose the trail.
export async function audited<R>(ref: AuditRef, run: () => Promise<R>, shape: (result: R) => unknown) {
  const line = { action: ref.action, target: ref.target, detail: ref.detail };
  try {
    const result = await run();
    audit(ref.admin, { ...line, ok: true });
    return shape(result);
  } catch (e) {
    audit(ref.admin, { ...line, ok: false, error: (e as Error).message });
    if (isForbidden(e)) return refused(e);
    return { action: { ok: false, notice: (e as Error).message } };
  }
}
