// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function bestEffort<T>(p: Promise<T>, fallback: T): Promise<T> {
  return p.catch(() => fallback);
}
