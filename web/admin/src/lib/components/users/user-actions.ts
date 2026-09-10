// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AccessKey } from '$lib/access';
import type { AdminUserWire } from '$lib/server/services';

// Every per-user mutation, as data.
//
// The inspector used to spell each of these out as its own <form> + <button> +
// disabled expression + toast handler, eight times, and they had already
// drifted: two of them forgot to disable while another verb was in flight, and
// the role gate was a hand-written `role === 'admin' || role === 'owner'`
// beside three of them and absent from the rest. Rendering one row per entry
// below removes the sibling-shaped duplication and makes "which role, which
// confirmation, which optimistic flip" answerable by reading one table.
//
// `action` is the server form-action name and is NOT free to rename: the audit
// trail keys off the matching spec name in +page.server.ts, and a rename here
// would silently 404 the POST rather than fail the build.
//
// `key` is a CLIENT visibility gate only. Every one of these is re-checked by
// requireRole in the action itself, and most again by the users service; a
// hidden button is a courtesy, not a boundary.

export type UserActionId =
  | 'toggleActive'
  | 'ban'
  | 'unban'
  | 'restart'
  | 'reset'
  | 'clearToken'
  | 'impersonate'
  | 'delete';

/** Which block of the inspector an action is drawn in. */
export type UserActionGroup = 'service' | 'support' | 'danger';

export type UserActionDef = {
  id: UserActionId;
  action: string;
  key: AccessKey;
  group: UserActionGroup;
  /** Catalog key for the button label. */
  label: string;
  /** Draws in the destructive style. */
  danger?: boolean;
  /**
   * Confirmation copy. Present exactly on the actions that reach outside this
   * console and cannot be undone from it: ban, delete, token clear, reset and
   * impersonate. Deletes get a dialog rather than an undo toast because the row
   * is not locally recreatable -- the users service has already dropped it.
   */
  confirm?: { title: string; body: string };
  /** Hidden entirely when false, e.g. Unban on a user who is not banned. */
  shown?: (u: AdminUserWire) => boolean;
  /**
   * The row as it will look once the server agrees. Applied instantly and
   * rolled back if it does not: optimistic, but truthful. Absent where the
   * outcome is not a row change (restart, impersonate) or where guessing would
   * be dishonest (token wipes -- the badge only clears on confirmation).
   */
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

/**
 * The label for `def` against a live row. Only the activate/deactivate toggle
 * reads the row, so this stays a lookup rather than a per-entry callback.
 */
export function actionLabel(def: UserActionDef, user: AdminUserWire): string {
  if (def.id !== 'toggleActive') return def.label;
  return user.is_active ? 'admin.users.deactivate' : 'admin.users.activate';
}

/** The actions of one group this role may see, for this row. */
export function actionsFor(
  group: UserActionGroup,
  user: AdminUserWire,
  can: (key: AccessKey) => boolean
): UserActionDef[] {
  return USER_ACTIONS.filter(
    (d) => d.group === group && can(d.key) && (d.shown?.(user) ?? true)
  );
}
