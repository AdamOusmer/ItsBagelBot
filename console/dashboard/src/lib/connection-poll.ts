// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

export type ConnectionPollGoal = 'connected' | 'disconnected';

export const CONNECTION_POLL_FAST_MS = 500;
export const CONNECTION_POLL_TIMEOUT_MS = 30_000;

const CONNECTION_POLL_SLOW_MS = 2500;
const CONNECTION_POLL_FAST_WINDOW_MS = 5000;
const CONNECTION_SETTLE_GRACE_MS = 1500;

export function connectionPollDelay(elapsedMs: number): number {
  return elapsedMs < CONNECTION_POLL_FAST_WINDOW_MS
    ? CONNECTION_POLL_FAST_MS
    : CONNECTION_POLL_SLOW_MS;
}

export function connectionPollSettled(
  goal: ConnectionPollGoal,
  state: string,
  elapsedMs: number,
  sawUnsettled: boolean
): boolean {
  if (goal === 'disconnected') {
    return state === 'unenrolled' || state === 'failing' || state === 'revoked';
  }
  return (
    state !== 'pending' &&
    state !== 'unenrolled' &&
    state !== 'unknown' &&
    (sawUnsettled || elapsedMs >= CONNECTION_SETTLE_GRACE_MS)
  );
}
