// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AccessKey } from '#lib/access';
import type { AdminUserWire } from '#lib/server/services';

// `action` is the server form-action name: a rename silently 404s the POST and splits the audit trail.

export type UserActionId =
  | 'toggleActive'
  | 'ban'
  | 'unban'
  | 'restart'
  | 'reset'
  | 'resetVoiceUsage'
  | 'setVoiceUsage'
  | 'clearToken'
  | 'impersonate'
  | 'delete';

export type UserActionGroup = 'service' | 'support' | 'danger' | 'voice';

export type UserActionDef = {
  id: UserActionId;
  action: string;
  key: AccessKey;
  group: UserActionGroup;
  label: string;
  danger?: boolean;
  confirm?: { title: string; body: string };
  shown?: (user: AdminUserWire) => boolean;
  optimistic?: (user: AdminUserWire) => AdminUserWire;
};

export const USER_ACTIONS: readonly UserActionDef[] = [
  {
    id: 'toggleActive',
    action: 'setActive',
    key: 'users.grant',
    group: 'service',
    label: 'admin.users.deactivate',
    optimistic: (user) => ({ ...user, is_active: !user.is_active })
  },
  {
    id: 'ban',
    action: 'ban',
    key: 'users.ban',
    group: 'service',
    label: 'admin.users.ban',
    danger: true,
    shown: (user) => !user.banned,
    confirm: { title: 'admin.users.confirmBanTitle', body: 'admin.users.confirmBanBody' },
    optimistic: (user) => ({ ...user, banned: true })
  },
  {
    id: 'unban',
    action: 'unban',
    key: 'users.ban',
    group: 'service',
    label: 'admin.users.unban',
    shown: (user) => user.banned,
    optimistic: (user) => ({ ...user, banned: false })
  },
  {
    id: 'restart',
    action: 'restart',
    key: 'users.restart',
    group: 'service',
    label: 'admin.users.restart'
  },
  {
    id: 'impersonate',
    action: 'impersonate',
    key: 'users.impersonate',
    group: 'support',
    label: 'admin.users.viewAsMint',
    confirm: {
      title: 'admin.users.confirmImpersonateTitle',
      body: 'admin.users.confirmImpersonateBody'
    }
  },
  {
    id: 'reset',
    action: 'reset',
    key: 'users.grant',
    group: 'danger',
    label: 'admin.users.reset',
    confirm: { title: 'admin.users.confirmResetTitle', body: 'admin.users.confirmResetBody' }
  },
  {
    id: 'resetVoiceUsage',
    action: 'setVoiceUsage',
    key: 'users.grant',
    group: 'voice',
    label: 'admin.users.voice.reset',
    confirm: { title: 'admin.users.voice.reset', body: 'admin.users.voice.resetConfirm' }
  },
  {
    id: 'setVoiceUsage',
    action: 'setVoiceUsage',
    key: 'users.grant',
    group: 'voice',
    label: 'admin.users.voice.set',
    confirm: { title: 'admin.users.voice.set', body: 'admin.users.voice.setConfirm' }
  },
  {
    id: 'clearToken',
    action: 'clearToken',
    key: 'users.token',
    group: 'danger',
    label: 'admin.users.clearToken',
    confirm: {
      title: 'admin.users.confirmClearTokenTitle',
      body: 'admin.users.confirmClearTokenBody'
    }
  },
  {
    id: 'delete',
    action: 'delete',
    key: 'users.delete',
    group: 'danger',
    label: 'admin.users.delete',
    danger: true,
    confirm: { title: 'admin.users.confirmDeleteTitle', body: 'admin.users.confirmDeleteBody' }
  }
];

export function actionLabel(action: UserActionDef, user: AdminUserWire): string {
  if (action.id === 'toggleActive' && !user.is_active) return 'admin.users.activate';
  return action.label;
}

export function actionsFor(
  group: UserActionGroup,
  user: AdminUserWire,
  can: (key: AccessKey) => boolean
): UserActionDef[] {
  return USER_ACTIONS.filter(
    (action) => action.group === group && can(action.key) && (action.shown?.(user) ?? true)
  );
}
