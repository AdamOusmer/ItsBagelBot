// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { NotificationWire } from '$lib/server/services';

export type NotificationLevel = NotificationWire['level'];
export type NotificationScope = NotificationWire['scope'];

export const LEVELS: readonly NotificationLevel[] = ['info', 'success', 'warning', 'critical'];

/** Level -> catalog key, so the picker and the row cannot name a level differently. */
export const LEVEL_LABEL = {
  info: 'admin.notifications.levelInfo',
  success: 'admin.notifications.levelSuccess',
  warning: 'admin.notifications.levelWarning',
  critical: 'admin.notifications.levelCritical'
} as const satisfies Record<NotificationLevel, string>;

/**
 * Level -> StatePill tone. The four levels borrow the tier palette rather than
 * inventing a fourth three-step scale, for the reason StatePill already gives
 * about the staff ladder: two palettes for two vocabularies is how they end up
 * disagreeing about which colour means "most urgent".
 */
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

/** The draft the composer edits. There is no "edit a sent notification" mode:
 *  the notifications service has no update verb -- a wrong message is retracted
 *  and resent -- so the inspector's only draft is a new one. */
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

/**
 * Whether `draft` carries enough to post. Mirrors sendFormError in
 * +page.server.ts: a direct message needs some identifier (either will do; the
 * send path resolves a username when only that is given), and every message
 * needs a title and a body.
 */
export function composeComplete(draft: ComposeDraft): boolean {
  if (!draft.title.trim() || !draft.body.trim()) return false;
  if (draft.scope !== 'direct') return true;
  return Boolean(draft.targetUserId.trim() || draft.targetUsername.trim());
}

/** Who a sent notification reached, for the row and the detail header. */
export function audienceOf(n: NotificationWire): { key: string; params: Record<string, string> } {
  if (n.scope === 'broadcast') return { key: 'admin.notifications.audienceAll', params: {} };
  return {
    key: 'admin.notifications.audienceUser',
    params: { id: String(n.target_user_id ?? '') }
  };
}
