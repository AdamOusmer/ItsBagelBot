// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { untrack } from 'svelte';
import { livePoll } from '@bagel/ui/lib/live-poll';

const POLL_MS = 3000;
const CLOCK_MS = 5000;

type Timed = { at: number; degraded: boolean };

export function liveBoard<S extends Timed>(initial: () => S, parse: (raw: unknown) => S | null, dataPath: () => string) {
  let snapshot = $state(untrack(initial));
  let trouble = $state(snapshot.degraded);
  let nowMs = $state(snapshot.at);
  let clockOffset = 0;

  function apply(raw: unknown): void {
    const next = parse(raw);
    trouble = !next || next.degraded;
    if (!next || next.degraded) return;
    clockOffset = Math.max(clockOffset, next.at - Date.now());
    snapshot = next;
    nowMs = Date.now() + clockOffset;
  }

  async function poll(): Promise<boolean> {
    if (document.hidden) return false;
    try {
      const res = await fetch(`${dataPath().replace(/\/$/, '')}/data`, { headers: { accept: 'application/json' } });
      apply(res.ok ? await res.json() : null);
    } catch {
      trouble = true;
    }
    return false;
  }

  function start(): () => void {
    clockOffset = snapshot.at - Date.now();
    const clock = setInterval(() => (nowMs = Date.now() + clockOffset), CLOCK_MS);
    const stopPoll = livePoll(poll, {
      firstDelayMs: POLL_MS,
      delayMs: () => POLL_MS,
      timeoutMs: Infinity,
      refreshOnVisible: true
    });
    return () => {
      clearInterval(clock);
      stopPoll();
    };
  }

  return {
    get snapshot() {
      return snapshot;
    },
    get trouble() {
      return trouble;
    },
    get nowMs() {
      return nowMs;
    },
    start
  };
}
