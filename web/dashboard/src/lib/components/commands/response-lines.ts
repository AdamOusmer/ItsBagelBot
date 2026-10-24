// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function splitPastedLines(text: string, room: number): string[] {
  const parts = text.split(/\r\n|\r|\n/);
  const keep = parts.slice(0, room + 1);
  const overflow = parts.slice(room + 1);
  if (overflow.length) keep[keep.length - 1] = [keep[keep.length - 1], ...overflow].join(' ');
  return keep;
}
