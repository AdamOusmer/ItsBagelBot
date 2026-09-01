// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// One deployment, four public hostnames, four completely different crawl
// stances. This module is the single place that names them, so /robots.txt and
// /sitemap.xml cannot disagree about which host they are answering for:
//
//   dashboard.itsbagelbot.com   the authed app. Nothing but the sign-in front
//                               door may be indexed.
//   stats.itsbagelbot.com       the public fleet odometer. Its root IS the
//                               stats page (see hooks.ts reroute), and that
//                               root is the canonical URL the page declares.
//   leaderboard.itsbagelbot.com one public board per channel at /<login>.
//                               Orphan URLs: nothing on the web links to them.
//   commands.itsbagelbot.com    the short host the bot hands out in chat
//                               (!cmd answers with <host>/user/<login>). Same
//                               /user/<login> route every hostname serves, on
//                               a branded name — so this is the one host where
//                               that route is opened to crawlers.
//
// All four are declared together in deploy/k8s/console-dashboard.yaml; keep
// this list in step with the Host() matchers there. An unlisted host is not
// merely unstyled, it is locked down (see the fallback in seoHost below).
//
// Before this existed there was a single static/robots.txt served to all four,
// written for the dashboard alone — so `Disallow: /` reached the leaderboard and
// commands hosts too and told crawlers to skip every channel board and command
// page they publish. Verified live before the fix: leaderboard.itsbagelbot.com
// answered 200 for a board and `Disallow: /` for robots.txt in the same breath.
//
// Matching the first label rather than a full hostname keeps local testing
// honest (stats.localhost, leaderboard.localhost) without a config knob, the
// same trick hooks.ts uses for the stats reroute.

import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';

export type SeoHost = 'dashboard' | 'stats' | 'leaderboard' | 'commands';

const KIND_BY_LABEL: Readonly<Record<string, SeoHost>> = {
  stats: 'stats',
  leaderboard: 'leaderboard',
  commands: 'commands'
};

/**
 * Which of the four surfaces this request landed on. Anything unrecognized —
 * a preview hostname, a raw pod IP, plain localhost — reads as 'dashboard',
 * the most restrictive stance, so a host we did not plan for is never the one
 * that accidentally opens the app to indexing.
 */
export function seoHost(url: URL): SeoHost {
  return KIND_BY_LABEL[url.hostname.split('.')[0] ?? ''] ?? 'dashboard';
}

/**
 * The origin a host publishes itself as. Hard-coded rather than read off the
 * request, matching the canonical <link> the stats and leaderboard pages
 * already emit: a preview or pod-IP hostname must advertise the production URL
 * or nothing, never itself.
 */
export const SEO_ORIGIN: Readonly<Record<SeoHost, string>> = {
  dashboard: 'https://dashboard.itsbagelbot.com',
  stats: 'https://stats.itsbagelbot.com',
  leaderboard: 'https://leaderboard.itsbagelbot.com',
  commands: 'https://commands.itsbagelbot.com'
};

/**
 * Send the visitor to `host`'s own origin unless they are already on it,
 * preserving path and query.
 *
 * The app answers its whole route table under every hostname traefik gives it,
 * so a page is only "on one host" if it says so itself. Without this, the same
 * document is four URLs — which is how leaderboard.itsbagelbot.com/user/<login>
 * came to serve the commands page under the leaderboard origin.
 *
 * 308 rather than 301: the permanent redirect that promises the method is not
 * rewritten. Path and query ride over verbatim, so a route's own canonical-form
 * redirects still get their turn once the request lands, and ?lang= survives.
 *
 * dev is exempt so localhost keeps serving the page it is asked for.
 */
export function requireHost(url: URL, host: SeoHost): void {
  if (dev || seoHost(url) === host) return;
  throw redirect(308, `${SEO_ORIGIN[host]}${url.pathname}${url.search}`);
}
