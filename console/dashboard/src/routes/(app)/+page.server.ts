import type { Actions, PageServerLoad } from './$types';
import { hasGrant, accountState, setActive, publishEventSub, auditImpersonation, type AccountStatus } from '$lib/server/rpc';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

export type ConnState = { enabled: boolean; receiving: boolean; status: AccountStatus };

// Resolve the bot connection state in one round trip (grant presence + the
// coalesced active/tier state_get). allSettled keeps a slow or down responder
// from failing the whole render; receiving stays gated on the grant.
async function connState(uid: string): Promise<ConnState> {
  const [grant, state] = await Promise.allSettled([hasGrant(uid), accountState(uid)]);
  const enabled = grant.status === 'fulfilled' && grant.value;
  const receiving = enabled && state.status === 'fulfilled' && state.value.active;
  const status: AccountStatus = state.status === 'fulfilled' ? state.value.status : 'free';
  return { enabled, receiving, status };
}

export const load: PageServerLoad = ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';

  // Return the RPC as an unawaited promise so SvelteKit streams it: the page
  // shell flushes immediately and the connection state hydrates when the round
  // trip lands, instead of blocking SSR (and the post-login redirect) on NATS.
  const conn: Promise<ConnState> =
    env.DEMO === '1'
      ? Promise.resolve({ enabled: true, receiving: true, status: 'vip' })
      : connState(uid);

  return { conn };
};

// When the session is an admin impersonation, record the write back to the
// acting admin (best-effort, fire-and-forget). No-op for a normal login.
function auditIfImpersonating(session: Session | null, action: string, detail: string): void {
  if (!session?.impersonator_id) return;
  auditImpersonation(
    session.impersonator_id,
    session.impersonator_login ?? '',
    `dashboard:${action}`,
    `${session.user_id}:${detail}`,
    ''
  );
}

export const actions: Actions = {
  // Enable: a single request to start event delivery. Marks the channel active
  // and (re)creates its EventSub subscriptions via the outgress lane.
  enable: async ({ locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await setActive(uid, true);
      await publishEventSub(uid, true);
      auditIfImpersonating(locals.session, 'enable', '');
      return { ok: true, action: 'enable' };
    } catch {
      return fail(502, { error: 'enable failed' });
    }
  },
  // Restart: delete + recreate the EventSub subscriptions (stays active).
  restart: async ({ locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await publishEventSub(uid, false);
      await publishEventSub(uid, true);
      auditIfImpersonating(locals.session, 'restart', '');
      return { ok: true, action: 'restart' };
    } catch {
      return fail(502, { error: 'restart failed' });
    }
  },
  // Disconnect: delete the subscriptions and mark inactive (grant kept).
  disconnect: async ({ locals }) => {
    const uid = locals.session?.user_id;
    if (!uid) return fail(401);
    try {
      await publishEventSub(uid, false);
      await setActive(uid, false);
      auditIfImpersonating(locals.session, 'disconnect', '');
      return { ok: true, action: 'disconnect' };
    } catch {
      return fail(502, { error: 'disconnect failed' });
    }
  }
};
