// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { ready as natsReady } from '@bagel/shared/server/nats';
import { ready as valkeyReady } from '@bagel/shared/server/valkey-store';
import { rateLimiterReady } from '@bagel/shared/server/rate-limit';

// External status endpoint for the Better Stack status page, same contract as
// the Go services' pkg/health /status: a named check per dependency, aggregate
// "ok" | "degraded" | "down", HTTP 503 only when down. Every check is hard:
// NATS because SSR cannot serve without RPC, and Valkey plus the rate limiter
// because the status page must show downtime when the cache tier is gone
// (the availability monitor only reacts to non-2xx). Pods still stay in
// rotation through an outage: /readyz is a separate static 200 and never
// reads these checks.

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
    status: down ? 503 : 200,
    headers: { 'content-type': 'application/json', 'cache-control': 'no-store' },
  });
};
