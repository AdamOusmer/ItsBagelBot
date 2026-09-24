// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Keep in step with the Host() matchers in deploy/k8s/console-dashboard.yaml.

import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';

export type SeoHost = 'dashboard' | 'stats' | 'leaderboard' | 'commands';

const KIND_BY_LABEL: Readonly<Record<string, SeoHost>> = {
  stats: 'stats',
  leaderboard: 'leaderboard',
  commands: 'commands'
};

export function seoHost(url: URL): SeoHost {
  return KIND_BY_LABEL[url.hostname.split('.')[0] ?? ''] ?? 'dashboard';
}

export const SEO_ORIGIN: Readonly<Record<SeoHost, string>> = {
  dashboard: 'https://dashboard.itsbagelbot.com',
  stats: 'https://stats.itsbagelbot.com',
  leaderboard: 'https://leaderboard.itsbagelbot.com',
  commands: 'https://commands.itsbagelbot.com'
};

export function requireHost(url: URL, host: SeoHost): void {
  if (dev || seoHost(url) === host) return;
  throw redirect(308, `${SEO_ORIGIN[host]}${url.pathname}${url.search}`);
}
