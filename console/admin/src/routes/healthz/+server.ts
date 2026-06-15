import type { RequestHandler } from './$types';

// Liveness/readiness probe target. No session/RPC dependency so a pod reports
// healthy as soon as the HTTP server is up.
export const GET: RequestHandler = () =>
  new Response('ok', { headers: { 'cache-control': 'no-store' } });
