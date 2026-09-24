// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { error } from '@sveltejs/kit';
import { requireRole } from '$lib/server/access';
import { deployApi, deployStatus, RunRelay } from '$lib/server/deploys';
import type { DeployRun, RunRequest } from '$lib/deploys/types';

const KEEPALIVE_MS = 20_000;

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
