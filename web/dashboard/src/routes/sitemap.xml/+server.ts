// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { seoHost, SEO_ORIGIN, type SeoHost } from '$lib/server/seo-hosts';

const PATHS: Readonly<Record<SeoHost, readonly string[]>> = {
  dashboard: [],
  stats: ['/'],
  leaderboard: [],
  commands: []
};

function urlset(origin: string, paths: readonly string[]): string {
  const entries = paths.map((p) => `<url><loc>${origin}${p}</loc></url>`).join('');
  return `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${entries}</urlset>`;
}

export const GET: RequestHandler = ({ url }) => {
  const host = seoHost(url);
  const paths = PATHS[host];
  if (paths.length === 0) throw error(404, 'Not found');

  return new Response(urlset(SEO_ORIGIN[host], paths), {
    headers: {
      'content-type': 'application/xml; charset=utf-8',
      'cache-control': 'public, max-age=0, s-maxage=86400, stale-while-revalidate=604800'
    }
  });
};
