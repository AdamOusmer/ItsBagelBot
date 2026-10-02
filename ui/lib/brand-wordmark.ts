// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function splitWordmark(title: string): [before: string, after: string] | null {
  const at = title.toLowerCase().lastIndexOf('o');
  return at < 0 ? null : [title.slice(0, at), title.slice(at + 1)];
}
