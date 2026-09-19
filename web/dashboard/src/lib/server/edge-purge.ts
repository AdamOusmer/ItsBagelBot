// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Best-effort Cloudflare purge-by-URL for the public command page. A toggle
// write already landed by the time this runs, so a purge failure must never
// fail the request: the caller falls back to the edge's stale-while-revalidate
// window and tells the visitor (`edgeDelayed`), it does not retry or throw.
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

// Gated on the build-time `dev` constant first, so Rollup erases this demo
// branch from production builds (see no-dev-code-in-prod).
const DEMO = dev && env.DEMO === '1';

const PURGE_TIMEOUT_MS = 3000;

/**
 * Purge one or more URLs from the Cloudflare edge cache (purge-by-URL,
 * available on every plan; the default cache key is scheme + host + path,
 * which is the key these pages use). Never throws: an unset zone/token, a
 * network failure or timeout, or a non-2xx response all resolve to `false`.
 */
export async function purgeEdge(urls: string[]): Promise<boolean> {
  if (DEMO) return true;

  const zoneId = env.CF_ZONE_ID;
  const token = env.CF_CACHE_PURGE_TOKEN;
  if (!zoneId || !token) return false;

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), PURGE_TIMEOUT_MS);
  try {
    const res = await fetch(`https://api.cloudflare.com/client/v4/zones/${zoneId}/purge_cache`, {
      method: 'POST',
      headers: {
        authorization: `Bearer ${token}`,
        'content-type': 'application/json'
      },
      body: JSON.stringify({ files: urls }),
      signal: controller.signal
    });
    return res.ok;
  } catch {
    return false;
  } finally {
    clearTimeout(timeout);
  }
}
