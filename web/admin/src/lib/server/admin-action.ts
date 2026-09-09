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

export function refusalReply(r: ParseRefusal) {
  if (r.refuse === 'bad-request') return fail(400, { error: r.message });
  return { action: { ok: false, notice: r.message } };
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
