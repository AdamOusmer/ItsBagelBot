// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Keep in step with the Host() matchers in deploy/k8s/console-dashboard.yaml.

export type SeoHost = 'dashboard' | 'stats' | 'leaderboard' | 'commands' | 'queue';

const KIND_BY_LABEL: Readonly<Record<string, SeoHost>> = {
  stats: 'stats',
  leaderboard: 'leaderboard',
  commands: 'commands',
  queue: 'queue'
};

export function seoHost(url: Pick<URL, 'hostname'>): SeoHost {
  return KIND_BY_LABEL[url.hostname.split('.')[0] ?? ''] ?? 'dashboard';
}
