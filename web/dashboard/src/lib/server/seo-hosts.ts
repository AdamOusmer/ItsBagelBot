// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { seoHost, type SeoHost } from '#lib/seo-host';

export { seoHost, type SeoHost };

export const SEO_ORIGIN: Readonly<Record<SeoHost, string>> = {
  dashboard: 'https://dashboard.itsbagelbot.com',
  stats: 'https://stats.itsbagelbot.com',
  leaderboard: 'https://leaderboard.itsbagelbot.com',
  commands: 'https://commands.itsbagelbot.com',
  queue: 'https://queue.itsbagelbot.com'
};

export function requireHost(url: URL, host: SeoHost): void {
  if (dev || seoHost(url) === host) return;
  throw redirect(308, `${SEO_ORIGIN[host]}${url.pathname}${url.search}`, { external: true });
}
