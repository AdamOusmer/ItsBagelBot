// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type SubState = 'ok' | 'pending' | 'failing' | 'revoked' | 'chat_banned' | 'unenrolled' | 'unknown';
export type PlanStatus = 'free' | 'paid' | 'vip';

export type ConnSignals = {
  grant: boolean | 'unknown';
  active: boolean | 'unknown';
  status: PlanStatus | 'unknown';
  sub: SubState;
};

export type ConnKind =
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
};

export function connectionUiState(s: ConnSignals): ConnUi {
  if (s.grant === 'unknown' || s.active === 'unknown') return ui('unavailable');
  if (!s.grant) return ui('auth_required');
  const blocked = blockedKind(s.sub);
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

const MANAGEABLE: readonly ConnKind[] = ['online', 'degraded', 'sub_unknown'];
const CONNECTABLE: readonly ConnKind[] = ['auth_required', 'reauth_required'];
const ENABLEABLE: readonly ConnKind[] = ['disabled', 'bot_banned'];
const RETRYABLE: readonly ConnKind[] = ['unavailable', 'sub_unknown'];

function ui(kind: ConnKind): ConnUi {
  return {
    kind,
    live: kind === 'online',
    canManage: MANAGEABLE.includes(kind),
    showEnable: ENABLEABLE.includes(kind),
    showConnect: CONNECTABLE.includes(kind),
    canRetry: RETRYABLE.includes(kind)
  };
}
