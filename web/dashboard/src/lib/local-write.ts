// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const ECHO_EXPIRY_MS = 3000;

type Expectation = { board: string; expiresAt: number };

let expected: Expectation[] = [];

// Each local write publishes one invalidation back; that one echo needs no refetch.
export function expectEcho(board: string): void {
  expected.push({ board, expiresAt: performance.now() + ECHO_EXPIRY_MS });
}

export function consumeEcho(board: string): boolean {
  const now = performance.now();
  expected = expected.filter((entry) => entry.expiresAt > now);
  const index = expected.findIndex((entry) => entry.board === board);
  if (index < 0) return false;
  expected.splice(index, 1);
  return true;
}
