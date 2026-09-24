// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AccessKey } from '$lib/access';
import type { AdminUserWire } from '$lib/server/services';

// `action` is the server form-action name: a rename silently 404s the POST and splits the audit trail.

export type UserActionId =
  | 'toggleActive'
  | 'ban'
  | 'unban'
  | 'restart'
  | 'reset'
  | 'clearToken'
  | 'impersonate'
  | 'delete';

export type UserActionGroup = 'service' | 'support' | 'danger';

export type UserActionDef = {
  id: UserActionId;
  action: string;
  key: AccessKey;
  group: UserActionGroup;
  label: string;
  danger?: boolean;
  confirm?: { title: string; body: string };
  shown?: (u: AdminUserWire) => boolean;
  optimistic?: (u: AdminUserWire) => AdminUserWire;
};

export const USER_ACTIONS: readonly UserActionDef[] = [
  {
    id: 'toggleActive',
    action: 'setActive',
    key: 'users.grant',
    group: 'service',
    label: 'admin.users.deactivate',
    optimistic: (u) => ({ ...u, is_active: !u.is_active })
  },
  {
    id: 'ban',
    action: 'ban',
    key: 'users.ban',
    group: 'service',
    label: 'admin.users.ban',
    danger: true,
    shown: (u) => !u.banned,
    confirm: { title: 'admin.users.confirmBanTitle', body: 'admin.users.confirmBanBody' },
    optimistic: (u) => ({ ...u, banned: true })
  },
  {
    id: 'unban',
    action: 'unban',
    key: 'users.ban',
    group: 'service',
    label: 'admin.users.unban',
    shown: (u) => u.banned,
    optimistic: (u) => ({ ...u, banned: false })
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

export function actionLabel(def: UserActionDef, user: AdminUserWire): string {
  if (def.id !== 'toggleActive') return def.label;
  return user.is_active ? 'admin.users.deactivate' : 'admin.users.activate';
}

export function actionsFor(
  group: UserActionGroup,
  user: AdminUserWire,
  can: (key: AccessKey) => boolean
): UserActionDef[] {
  return USER_ACTIONS.filter(
    (d) => d.group === group && can(d.key) && (d.shown?.(user) ?? true)
  );
}
