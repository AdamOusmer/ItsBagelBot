// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type SubState = 'ok' | 'pending' | 'failing' | 'revoked' | 'chat_banned' | 'unenrolled' | 'unknown';
export type PlanStatus = 'free' | 'paid' | 'vip';

export type ChannelLifecycle = {
 version: number; desired_enabled: boolean; runtime_state: string;
 chat_restriction: string; grant_health: string; timeout_until?: string;
 incident_reason?: string; last_failure?: string; control_listeners_healthy?: boolean;
};

export type ConnSignals = {
 lifecycle?: ChannelLifecycle;
 lifecycleRevision?: number;
 accountCreatedAt?: number;
  grant: boolean | 'unknown';
  active: boolean | 'unknown';
  status: PlanStatus | 'unknown';
  sub: SubState;
};

export type ConnKind =
  | 'dormant'
  | 'timed_out'
  | 'recovering'
  | 'unavailable'
  | 'auth_required'
  | 'reauth_required'
  | 'bot_banned'
  | 'disabled'
  | 'connecting'
  | 'online'
  | 'degraded'
  | 'sub_unknown';

export type ConnUi = {
  kind: ConnKind;
  live: boolean;
  canManage: boolean;
  showEnable: boolean;
  showConnect: boolean;
  canRetry: boolean;
  verifyRepair: boolean;
  retainedBan: boolean;
  grantNeedsReconnect: boolean;
  timeoutUntil?: string;
  automaticRecovery: boolean;
};

export function connectionUiState(s: ConnSignals): ConnUi {
 const lifecycle=s.lifecycle;
 if (!lifecycle) return legacyConnectionUi(s);
 if (lifecycle.version !== 1) return ui('unavailable');
 return lifecycleConnectionUi(s,lifecycle);
}
function lifecycleConnectionUi(s: ConnSignals,l: ChannelLifecycle): ConnUi {
 if (!l.desired_enabled) return disabledLifecycleUi(l);
 const restriction=restrictionUi(l);
 if (restriction) return restriction;
 const transition=transitionUi(l);
 if (transition) return transition;
 if (l.grant_health === 'missing') return ui('reauth_required');
 if (l.runtime_state === 'blocked') return ui('degraded');
 if (l.runtime_state === 'active') return activeLifecycleUi(s);
 return legacyConnectionUi(s);
}
function disabledLifecycleUi(l: ChannelLifecycle): ConnUi {
 return {...ui('disabled'),showEnable:true,retainedBan:l.chat_restriction === 'banned',grantNeedsReconnect:l.grant_health === 'missing'};
}
function restrictionUi(l: ChannelLifecycle): ConnUi | undefined {
 if (l.chat_restriction === 'banned') return {...ui('bot_banned'),grantNeedsReconnect:l.grant_health === 'missing',automaticRecovery:l.control_listeners_healthy === true};
 if (l.chat_restriction === 'timeout') return {...ui('timed_out'),timeoutUntil:l.timeout_until};
 return undefined;
}
function transitionUi(l: ChannelLifecycle): ConnUi | undefined {
 switch(l.runtime_state) {
  case 'recovering': return ui('recovering');
  case 'degraded': return ui('degraded');
  case 'dormant': return ui('dormant');
  default: return undefined;
 }
}
function activeLifecycleUi(s: ConnSignals): ConnUi {
 // The authoritative healthy lifecycle outranks stale compatibility flags.
 const sub=s.sub === 'chat_banned' || s.sub === 'revoked' ? 'unknown' : s.sub;
 return ui(activeKind(sub));
}
function legacyConnectionUi(s: ConnSignals): ConnUi {
 if (s.sub === 'chat_banned') return ui('bot_banned');
 if (s.grant === 'unknown' || s.active === 'unknown') return ui('unavailable');
 if (!s.grant) return ui('auth_required');
 const blocked=blockedKind(s.sub);
 if (blocked) return ui(blocked);
 if (!s.active) return ui('disabled');
 return ui(activeKind(s.sub));
}

function blockedKind(sub: SubState): ConnKind | null {
  switch (sub) {
    case 'revoked':
      return 'reauth_required';
    case 'chat_banned':
      return 'bot_banned';
    default:
      return null;
  }
}

function activeKind(sub: SubState): ConnKind {
  switch (sub) {
    case 'ok':
      return 'online';
    case 'failing':
      return 'degraded';
    case 'pending':
    case 'unenrolled':
      return 'connecting';
    default:
      return 'sub_unknown';
  }
}

const MANAGEABLE: readonly ConnKind[] = ['online', 'dormant', 'sub_unknown'];
const CONNECTABLE: readonly ConnKind[] = ['auth_required', 'reauth_required'];
const ENABLEABLE: readonly ConnKind[] = ['disabled'];
const RETRYABLE: readonly ConnKind[] = ['unavailable', 'sub_unknown'];

function ui(kind: ConnKind): ConnUi {
  return {
    kind,
    live: kind === 'online',
    canManage: MANAGEABLE.includes(kind),
    showEnable: ENABLEABLE.includes(kind),
    showConnect: CONNECTABLE.includes(kind),
    canRetry: RETRYABLE.includes(kind),
    verifyRepair: ['bot_banned', 'degraded', 'timed_out'].includes(kind),
    retainedBan: false,
    grantNeedsReconnect: false,
    automaticRecovery: false
  };
}

// Poll replies and loader refreshes use the same Users revision and incarnation.
export function mergeConnectionSignals(loader: ConnSignals, polled?: Partial<ConnSignals> | null): ConnSignals {
 if (!polled) return loader;
 const merged={...loader,...polled,lifecycle:polled.lifecycle ?? loader.lifecycle};
 if (!loader.lifecycle) return merged;
 if ((loader.accountCreatedAt ?? 0) > (polled.accountCreatedAt ?? 0)) return loader;
 if (loader.accountCreatedAt !== polled.accountCreatedAt) return merged;
 if ((loader.lifecycleRevision ?? 0) >= (polled.lifecycleRevision ?? 0)) return loader;
 return merged;
}
