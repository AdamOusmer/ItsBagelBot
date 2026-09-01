// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { seoHost, type SeoHost } from '$lib/server/seo-hosts';

// Per-host robots.txt, replacing static/robots.txt.
//
// It had to stop being a static file: SvelteKit serves static/ from one build
// to all four hostnames, so the dashboard's `Disallow: /` was also the answer
// leaderboard.itsbagelbot.com gave for every channel board it publishes — the
// pages exist, render server-side for exactly this reason, and were told to
// every crawler as off-limits. A static file cannot say four things.
//
// The rule applied below: each public page is opened on exactly ONE host, the
// one its URL is handed out under, and disallowed on the other three. The app
// serves every route on every hostname, so without that the same document is
// four URLs and search engines pick the winner themselves. /user/<login> is the
// case that matters — the bot posts commands.itsbagelbot.com/user/<login> in
// chat, so that is its host, and the other three disallow it.

const DASHBOARD = `# ItsBagelBot Dashboard — https://dashboard.itsbagelbot.com
# A private, auth-gated app. Only the public sign-in landing should be indexed;
# everything else sits behind login and must not be crawled or indexed.

User-agent: *
Allow: /$
Allow: /login
Disallow: /
`;

const STATS = `# ItsBagelBot Stats — https://stats.itsbagelbot.com
# The root IS the public stats page (hooks.ts reroutes '/' to /stats on this
# host) and is the canonical URL the page declares, so the root is the one
# document to index. Everything else this host answers is either the same page
# under its unpinned /stats path, a data or SSE endpoint behind it, or an authed
# route that redirects to sign-in — none of them search results.
#
# /sitemap.xml is allowed explicitly: a sitemap that robots.txt disallows cannot
# be fetched, which would make the Sitemap line below point at a closed door.

User-agent: *
Allow: /$
Allow: /sitemap.xml
Disallow: /

Sitemap: https://stats.itsbagelbot.com/sitemap.xml
`;

const LEADERBOARD = `# ItsBagelBot Leaderboards — https://leaderboard.itsbagelbot.com
# One public board per channel at /<login>, each declaring this host in its
# canonical URL. They are meant to be found, so the default here is Allow.
#
# No Sitemap line: these URLs cannot be enumerated yet (the users service
# exposes no channel listing), and a sitemap with an empty urlset is an error
# against the property rather than a neutral no-op.

User-agent: *
Allow: /

# Not documents: JSON and SSE endpoints. /stats/stream in particular holds a
# connection open for as long as the client keeps reading, which a crawler will.
Disallow: /stats/
Disallow: /events

# Session machinery. Crawling these spends budget on redirects to sign-in and,
# for the two preference writes, on setting cookies for a client that has none.
Disallow: /auth/
Disallow: /delegate/
Disallow: /lang
Disallow: /cursor

# Health and monitoring surfaces.
Disallow: /healthz
Disallow: /readyz
Disallow: /status

# Public, SSR'd, and still blocked here: /user/<login> is handed out by the bot
# under commands.itsbagelbot.com, which is where it is indexed. The app answers
# it on this host too, and indexing it twice would be indexing one page as two.
Disallow: /user/
`;

const COMMANDS = `# ItsBagelBot Commands — https://commands.itsbagelbot.com
# The short host the bot hands out in chat: !cmd answers with
# commands.itsbagelbot.com/user/<login>, a channel's public command page. That
# is the only document this host publishes; the app answers its other routes
# here as it does everywhere, and every one of them belongs to another host.
#
# No Sitemap line: as with the leaderboard, one URL exists per enrolled channel
# and the users service exposes no listing to enumerate them yet.

User-agent: *
Allow: /user/
Disallow: /
`;

const BODY: Readonly<Record<SeoHost, string>> = {
  dashboard: DASHBOARD,
  stats: STATS,
  leaderboard: LEADERBOARD,
  commands: COMMANDS
};

export const GET: RequestHandler = ({ url }) =>
  // harden() in hooks.server.ts forces no-store on HTML and redirects only, so
  // this Cache-Control survives to the edge. Crawl rules change on deploys, not
  // on traffic, so a long shared TTL costs nothing and keeps bot fetches off
  // the pods entirely.
  new Response(BODY[seoHost(url)], {
    headers: {
      'content-type': 'text/plain; charset=utf-8',
      'cache-control': 'public, max-age=0, s-maxage=86400, stale-while-revalidate=604800'
    }
  });
