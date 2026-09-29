// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// CDN-Cache-Control, never s-maxage: Cloudflare disables stale-while-revalidate when s-maxage is present.
export const STATS_EDGE_HEADERS = {
  'cache-control': 'public, max-age=0',
  'cdn-cache-control': 'max-age=2, stale-while-revalidate=10, stale-if-error=60'
} as const;
