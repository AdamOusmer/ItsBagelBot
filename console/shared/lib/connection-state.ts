// Honest bot-connection state. The dashboard used to fold three separate reads
// (Twitch grant, users-service active flag, outgress enroll state) into a single
// `receiving` boolean with `subState !== 'unenrolled'`, which let `pending`,
// `failing`, and `unknown` all read as "Online · in chat", and defaulted a
// failed account read to "Free". This module keeps every read's failure visible
// as `'unknown'` and maps the honest signals to exactly one UI state, so a
// down/pending/failing connection can never masquerade as online.
//
// Pure and framework-free: the server computes it for SSR, the client recomputes
// it from the /substate poll, and the unit test drives the whole permutation
// matrix. String-literal types mirror ChannelSubState['state'] and AccountStatus
// in dashboard $lib/server/services.ts (kept in sync by hand, both closed sets).

export type SubState = 'ok' | 'pending' | 'failing' | 'revoked' | 'unenrolled' | 'unknown';
export type PlanStatus = 'free' | 'paid' | 'vip';

// A read that failed (RPC down / timeout) surfaces as 'unknown', never a silent
// default. `grant`/`active` are tri-state; `status`/`sub` carry their own
// 'unknown' member.
export type ConnSignals = {
  grant: boolean | 'unknown';
  active: boolean | 'unknown';
  status: PlanStatus | 'unknown';
  sub: SubState;
};

export type ConnKind =
  | 'unavailable' // a core read (grant or active) is down — we cannot tell
  | 'auth_required' // no Twitch grant on file
  | 'reauth_required' // Twitch revoked the grant (password change / app disconnect); user must re-consent
  | 'disabled' // grant present but the channel is inactive (disconnected)
  | 'connecting' // active, enroll in flight (pending / just-published)
  | 'online' // active + enroll ok — the ONLY truthful "online"
  | 'degraded' // active + enroll failing (connected but not in chat)
  | 'sub_unknown'; // active but the enroll read is unavailable

export type ConnUi = {
  kind: ConnKind;
  live: boolean; // the green "in chat" dot — online only
  canManage: boolean; // channel is active: restart / disconnect apply
  showEnable: boolean; // the ?/enable form (disconnected channel, nothing in flight)
  showConnect: boolean; // route to Settings for Twitch authorization
  canRetry: boolean; // a read is unavailable: offer a refresh, not a stale value
};

// connectionUiState maps honest signals to exactly one UI state. Every backend
// permutation resolves here to one kind → one headline → one action. The core
// invariant: `online` requires grant true, active true, AND sub === 'ok'.
export function connectionUiState(s: ConnSignals): ConnUi {
  // A down core read must not be reported as any definite connection state.
  if (s.grant === 'unknown' || s.active === 'unknown') return ui('unavailable');
  if (!s.grant) return ui('auth_required');
  if (!s.active) return ui('disabled');
  // Active from here on; the enroll state decides whether chat is actually served.
  return ui(activeKind(s.sub));
}

// activeKind maps an active channel's enroll state to its UI kind.
function activeKind(sub: SubState): ConnKind {
  switch (sub) {
    case 'ok':
      return 'online';
    case 'failing':
      return 'degraded';
    // Twitch revoked the broadcaster's authorization. Restart cannot fix it
    // (outgress skips enrolls for revoked channels on purpose); only a fresh
    // Twitch consent can, so the UI routes to reconnect instead of retry.
    case 'revoked':
      return 'reauth_required';
    case 'pending':
    case 'unenrolled':
      return 'connecting';
    default:
      return 'sub_unknown';
  }
}

// Per-kind action sets. Membership lists keep each ConnUi flag a single
// readable predicate instead of a growing boolean chain.
const MANAGEABLE: readonly ConnKind[] = ['online', 'degraded', 'sub_unknown', 'connecting'];
const CONNECTABLE: readonly ConnKind[] = ['auth_required', 'reauth_required'];
const RETRYABLE: readonly ConnKind[] = ['unavailable', 'sub_unknown'];

function ui(kind: ConnKind): ConnUi {
  return {
    kind,
    live: kind === 'online',
    canManage: MANAGEABLE.includes(kind),
    showEnable: kind === 'disabled',
    showConnect: CONNECTABLE.includes(kind),
    canRetry: RETRYABLE.includes(kind)
  };
}
