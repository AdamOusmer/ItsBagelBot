// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The admin console's staff ladder, in one place.
//
// It lives in @bagel/shared rather than in the admin app because two things
// have to agree about it and they are on opposite sides of the client/server
// line: access.ts (ROLE_FOR, the server-side authorization table) and
// nav-admin.ts (which sections a role may SEE). When they disagreed, the nav
// offered a link the route then bounced -- that is exactly what a moderator saw
// for /staff, /audit and /secrets before this ladder was shared.
//
// Pure data, no imports: safe in either app's boot import graph.

export type StaffRole = 'moderator' | 'admin' | 'owner';

/** Ascending authority. Comparisons are always `>=`, never equality. */
export const STAFF_RANK: Record<StaffRole, number> = {
  moderator: 1,
  admin: 2,
  owner: 3
};

/** True when `role` sits at or above `min` on the ladder. */
export function staffAtLeast(role: StaffRole, min: StaffRole): boolean {
  return STAFF_RANK[role] >= STAFF_RANK[min];
}
