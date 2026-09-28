// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function matchesLogin(typed: string, login: string): boolean {
  const expected = login.trim().toLowerCase();
  return expected !== '' && typed.trim().toLowerCase() === expected;
}
