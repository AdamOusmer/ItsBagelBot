// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

const DISCONNECTED_STATES=['unenrolled','failing','revoked','disabled'];
const CONNECTING_STATES=['recovering','pending','unenrolled','unknown'];

export function connectionPollSettled(goal:ConnectionPollGoal,state:string,elapsedMs:number,sawUnsettled:boolean):boolean {
 if (goal === 'disconnected') return DISCONNECTED_STATES.includes(state);
 if (CONNECTING_STATES.includes(state)) return false;
 return sawUnsettled || elapsedMs >= CONNECTION_SETTLE_GRACE_MS;
}
