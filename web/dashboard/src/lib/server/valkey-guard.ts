// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { CircuitBreaker, withTimeout } from '@bagel/kit/server/resilience';

const OP_TIMEOUT_MS = 200;

export function valkeyGuard(breakerName: string, timeoutLabel: string): <T>(op: () => Promise<T>) => Promise<T> {
  const breaker = new CircuitBreaker({ name: breakerName, failureThreshold: 3, resetMs: 5_000 });
  return (op) => breaker.run(() => withTimeout(op(), OP_TIMEOUT_MS, timeoutLabel));
}
