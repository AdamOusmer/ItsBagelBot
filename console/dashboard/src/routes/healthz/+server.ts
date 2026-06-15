import type { RequestHandler } from './$types';

// Liveness/readiness probe target. Plain 200 with no caching and no session or
// RPC dependency so a pod reports healthy as soon as the HTTP server is up.
export const GET: RequestHandler = () =>
  new Response('ok', { headers: { 'cache-control': 'no-store' } });
