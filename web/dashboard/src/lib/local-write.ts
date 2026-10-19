// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const ECHO_WINDOW_MS = 1500;

let lastWrite = -Infinity;

export function noteLocalWrite(): void {
  lastWrite = performance.now();
}

export function isLocalEcho(): boolean {
  return performance.now() - lastWrite < ECHO_WINDOW_MS;
}
