// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AdminRole } from '$lib/access';

// Role -> catalog key. A flat table, not a ternary chain: three roles today,
// and a fourth would otherwise nest. The same three keys already label the
// signed-in operator in (admin)/+layout.svelte, so the roster and the shell
// cannot call the same role two different things.
export const ROLE_LABEL = {
  moderator: 'admin.roleModerator',
  admin: 'admin.roleAdmin',
  owner: 'admin.roleOwner'
} as const satisfies Record<AdminRole, string>;

/** The draft the inspector edits, for both an existing member and a new one. */
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

/**
 * Whether `draft` carries enough to post. Mirrors the upsert action's own
 * validation (numeric id, non-empty login) so Save is not offered for a body
 * the server will answer 400 to.
 */
export function draftComplete(draft: StaffDraft): boolean {
  return /^[0-9]+$/.test(draft.userId) && draft.login.trim().length > 0;
}
