// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { ready as natsReady } from '@bagel/shared/server/nats';
import { ready as valkeyReady } from '@bagel/shared/server/valkey-store';
import { rateLimiterReady } from '@bagel/shared/server/rate-limit';

// External status endpoint for the Better Stack status page, same contract as
// the Go services' pkg/health /status: a named check per dependency, aggregate
// "ok" | "degraded" | "down", and an HTTP status code carrying that same
// verdict so a monitor never has to read the body: 200 ok, 207 degraded,
// 503 down.
//
// 207 (Multi-Status) is the honest code for a mixed answer and, being 2xx, it
// still reads green to any plain "expect 2xx" check. Better Stack then splits
// paging from notifying on the expected-status-code list alone: an availability
// monitor expecting "200,207" pages only on a real outage, while a second
// monitor expecting "200" notifies on the impairment without waking anyone.
// The earlier design left degraded on 200 and put the word in the body, which
// needed a keyword monitor; a status code costs nothing on the free plan and
// cannot drift out of sync with the aggregate the way a body string can.
//
// Every check is hard: NATS because SSR cannot serve without RPC, and Valkey
// plus the rate limiter because the status page must show downtime when the
// cache tier is gone. This endpoint answers for the console only and
// deliberately does not fan out to the other services: each one publishes its
// own /status, so probing them here would report their outages a second time
// under the console's name. Pods still stay in rotation through an outage:
// /readyz is a separate static 200 and never reads these checks.

// One table for both halves of the verdict, so the aggregate in the body and
// the code on the wire cannot drift apart as checks are added. degraded is in
// the table before it can fire: every check here is hard today, so down covers
// any failure, and the first optional check starts serving 207 on its own.
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
