// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { NotificationWire } from '$lib/server/services';

export type NotificationLevel = NotificationWire['level'];
export type NotificationScope = NotificationWire['scope'];

export const LEVELS: readonly NotificationLevel[] = ['info', 'success', 'warning', 'critical'];

export const LEVEL_LABEL = {
  info: 'admin.notifications.levelInfo',
  success: 'admin.notifications.levelSuccess',
  warning: 'admin.notifications.levelWarning',
  critical: 'admin.notifications.levelCritical'
} as const satisfies Record<NotificationLevel, string>;

export const LEVEL_TONE = {
  info: 'neutral',
  success: 'free',
  warning: 'paid',
  critical: 'banned'
} as const satisfies Record<NotificationLevel, string>;

export const SCOPE_LABEL = {
  broadcast: 'admin.notifications.scopeBroadcast',
  direct: 'admin.notifications.scopeDirect'
} as const satisfies Record<NotificationScope, string>;

export type ComposeDraft = {
  scope: NotificationScope;
  targetUserId: string;
  targetUsername: string;
  title: string;
  body: string;
  level: NotificationLevel;
  expiresAt: string;
};

export const NEW_NOTIFICATION = '__new__';

export function blankCompose(): ComposeDraft {
  return {
    scope: 'broadcast',
    targetUserId: '',
    targetUsername: '',
    title: '',
    body: '',
    level: 'info',
    expiresAt: ''
  };
}

export function composeComplete(draft: ComposeDraft): boolean {
  if (!draft.title.trim() || !draft.body.trim()) return false;
  if (draft.scope !== 'direct') return true;
  return Boolean(draft.targetUserId.trim() || draft.targetUsername.trim());
}

export function audienceOf(n: NotificationWire): { key: string; params: Record<string, string> } {
  if (n.scope === 'broadcast') return { key: 'admin.notifications.audienceAll', params: {} };
  return {
    key: 'admin.notifications.audienceUser',
    params: { id: String(n.target_user_id ?? '') }
  };
}
