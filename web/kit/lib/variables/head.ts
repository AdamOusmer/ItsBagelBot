// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function tokenHead(token: string): string {
  const inner = /^\{([^}]*)\}$/.exec(token.trim())?.[1];
  if (!inner) return '';
  return inner.split(':', 1)[0].trim().toLowerCase();
}
