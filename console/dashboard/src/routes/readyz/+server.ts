import type { RequestHandler } from './$types';
import { ready } from '@bagel/shared/server/nats';
import { ready as warmValkey } from '@bagel/shared/server/valkey-store';

export const GET: RequestHandler = async () => {
  // Gate readiness on NATS (the hard dependency) and best-effort-warm the Valkey
  // read pool on the same probe. Valkey is NOT a readiness gate: it is an
  // optional tier the read path degrades past to RPC, so a Valkey outage must
  // not pull every replica out of rotation. Re-warming each probe means a
  // rotated-in pod's pool is hot within one interval, so the first real request
  // rarely pays a cold connect.
  const [natsReady] = await Promise.all([ready(), warmValkey()]);
  if (!natsReady) {
    return new Response('not ready', { status: 503, headers: { 'cache-control': 'no-store' } });
  }
  return new Response('ok', { headers: { 'cache-control': 'no-store' } });
};
