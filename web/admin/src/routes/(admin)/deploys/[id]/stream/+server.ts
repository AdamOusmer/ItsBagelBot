// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { error } from '@sveltejs/kit';
import { requireRole } from '$lib/server/access';
import { deployApi, deployStatus, RunRelay } from '$lib/server/deploys';
import type { DeployRun, RunRequest } from '$lib/deploys/types';

// Matches the events stream: often enough that no proxy on the tailnet path
// idles the connection out during a quiet stage (a build can sit on one job
// for minutes without a state change).
const KEEPALIVE_MS = 20_000;

// SSE bridge for one run: the current snapshot first, then one `run` frame per
// state change from bagel.deploy.events.<id>. Every frame is a full snapshot,
// so a client that reconnects needs nothing but this endpoint again.
export const GET: RequestHandler = async ({ locals, params }) => {
  const admin = await requireRole({ locals }, 'deploys.manage');
  if (!admin) throw error(403, 'forbidden');

  const api = await deployApi();
  const req: RunRequest = { actor_id: admin.id, run_id: params.id };
  const relay = new RunRelay();
  const unwatch = api.watch(req.run_id, {
    run: (run) => relay.offer(run),
    gap: () => {
      api.get(req).then(
        (run) => relay.offer(run),
        () => {}
      );
    }
  });

  let snapshot: DeployRun;
  try {
    snapshot = await api.get(req);
  } catch (e) {
    unwatch();
    throw error(deployStatus(e), (e as Error).message);
  }

  let keepalive: ReturnType<typeof setInterval> | undefined;
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      relay.open(controller, snapshot);
      keepalive = setInterval(() => relay.keepalive(), KEEPALIVE_MS);
    },
    cancel() {
      clearInterval(keepalive);
      unwatch();
    }
  });
  return new Response(stream, {
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-store',
      Connection: 'keep-alive'
    }
  });
};
