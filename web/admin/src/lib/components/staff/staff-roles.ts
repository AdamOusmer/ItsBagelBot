// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AdminRole } from '$lib/access';

export const ROLE_LABEL = {
  moderator: 'admin.roleModerator',
  admin: 'admin.roleAdmin',
  owner: 'admin.roleOwner'
} as const satisfies Record<AdminRole, string>;

export type StaffDraft = {
  userId: string;
  login: string;
  displayName: string;
  role: AdminRole;
};

export const NEW_MEMBER = '__new__';

export function blankDraft(role: AdminRole): StaffDraft {
  return { userId: '', login: '', displayName: '', role };
}

export function draftComplete(draft: StaffDraft): boolean {
  return /^[0-9]+$/.test(draft.userId) && draft.login.trim().length > 0;
}
