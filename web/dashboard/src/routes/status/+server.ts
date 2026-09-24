// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { ready as natsReady } from '@bagel/kit/server/nats';
import { ready as valkeyReady } from '@bagel/kit/server/valkey-store';
import { rateLimiterReady } from '@bagel/kit/server/rate-limit';

// Same contract as pkg/health /status (200 ok, 207 degraded, 503 down): Better Stack keys off it.

const HTTP_STATUS = { ok: 200, degraded: 207, down: 503 } as const;

interface CheckResult {
  name: string;
  ok: boolean;
  optional?: boolean;
  latency_ms: number;
}

async function runCheck(
  name: string,
  probe: () => Promise<boolean>,
  optional = false
): Promise<CheckResult> {
  const start = performance.now();
  let ok = false;
  try {
    ok = await probe();
  } catch {
    ok = false;
  }
  const result: CheckResult = { name, ok, latency_ms: Math.round(performance.now() - start) };
  if (optional) result.optional = true;
  return result;
}

export const GET: RequestHandler = async () => {
  const checks = await Promise.all([
    runCheck('nats', () => natsReady()),
    runCheck('valkey', () => valkeyReady()),
    runCheck('rate_limiter', () => rateLimiterReady()),
  ]);

  const down = checks.some((c) => !c.ok && !c.optional);
  const degraded = checks.some((c) => !c.ok);
  const status = down ? 'down' : degraded ? 'degraded' : 'ok';

  return new Response(JSON.stringify({ service: 'console-dashboard', status, checks }), {
    status: HTTP_STATUS[status],
    headers: { 'content-type': 'application/json', 'cache-control': 'no-store' },
  });
};
