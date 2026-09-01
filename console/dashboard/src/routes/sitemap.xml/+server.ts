// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { seoHost, SEO_ORIGIN, type SeoHost } from '$lib/server/seo-hosts';

// Per-host sitemap. The two content sites (itsbagelbot.com, docs.) build theirs
// at compile time with @astrojs/sitemap; this app cannot, because which URLs
// exist depends on the hostname the request arrived on and SvelteKit renders
// all four from one build.
//
// Only hosts with something to declare get a document. A host with an empty
// urlset is worse than no sitemap at all — Search Console reports it as an
// error against the property and keeps reporting it — so those 404 instead,
// and robots.txt omits the `Sitemap:` line for them to match.

/**
 * Paths each host publishes, relative to its own origin.
 *
 * dashboard   — nothing. Every page is behind the session gate; /login is the
 *               one public document and a sign-in form is not a search result.
 * stats       — the root only. /stats answers the same page on this host, but
 *               the page's canonical <link> points at the root, so listing both
 *               would put a URL in the sitemap that canonicalizes away.
 * leaderboard — empty for now, and the gap worth closing. One /<login> board per
 * commands      enrolled channel, one /user/<login> command page likewise, and
 *               both are orphan URLs: nothing on the web links to them, and the
 *               bot hands the command page out in chat, which no crawler reads.
 *               A sitemap is their ONLY discovery path.
 *
 *               Listing them needs a "list enrolled channels" read the users
 *               service does not expose yet. Deriving it from publicBoards()
 *               instead was rejected: that is the ~10 channels currently topping
 *               /stats, so the sitemap would churn every cache window and cap
 *               coverage at ten while looking complete.
 */
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

  // harden() in hooks.server.ts forces no-store on HTML and redirects only, so
  // this Cache-Control survives to the edge. A day is far longer than the app's
  // page TTLs on purpose: the contents change when a host gains a surface, not
  // when its numbers move.
  return new Response(urlset(SEO_ORIGIN[host], paths), {
    headers: {
      'content-type': 'application/xml; charset=utf-8',
      'cache-control': 'public, max-age=0, s-maxage=86400, stale-while-revalidate=604800'
    }
  });
};
