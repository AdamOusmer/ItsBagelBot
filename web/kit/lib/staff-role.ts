// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type StaffRole = 'moderator' | 'admin' | 'owner';

export const STAFF_RANK: Record<StaffRole, number> = {
  moderator: 1,
  admin: 2,
  owner: 3
};

export function staffAtLeast(role: StaffRole, min: StaffRole): boolean {
  return STAFF_RANK[role] >= STAFF_RANK[min];
}
