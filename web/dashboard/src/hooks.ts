// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Reroute } from '@sveltejs/kit/hooks';

const QUEUE_PATH = /^\/(@?\w+)(\/data)?\/?$/;

const TOP_LEVEL_ROUTES: ReadonlySet<string> = new Set([
  'auth', 'cursor', 'delegate', 'events', 'goodbye', 'healthz', 'lang', 'login', 'queue', 'readyz', 'spotify', 'stats', 'status', 'user', 'welcome'
]);

function queueRoute(pathname: string): string | undefined {
  const match = QUEUE_PATH.exec(pathname);
  if (!match || TOP_LEVEL_ROUTES.has(match[1])) return undefined;
  return `/queue/${match[1]}${match[2] ?? ''}`;
}

export const reroute: Reroute = ({ url }) => {
  const label = url.hostname.split('.')[0];
  if (label === 'stats' && url.pathname === '/') return '/stats';
  if (label === 'queue') return queueRoute(url.pathname);
};
