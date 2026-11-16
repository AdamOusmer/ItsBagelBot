// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
type Navigation = { key: string; index: number; length: number };

export function resultIndex({ key, index, length }: Navigation): number {
  if (length === 0) return -1;
  const delta = key === 'ArrowUp' ? -1 : 1;
  return (index + delta + length) % length;
}
