// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function perchSplit(status: number | string): { before: string; digit: string; after: string } {
  const code = String(status);
  const middle = Math.floor(code.length / 2);
  return { before: code.slice(0, middle), digit: code.charAt(middle), after: code.slice(middle + 1) };
}
