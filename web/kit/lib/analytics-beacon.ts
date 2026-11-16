// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { MARKETING_CTAS, PUBLIC_PAGES, type BeaconEvent, type PublicPage } from './analytics-beacon-catalog';

const PUBLIC_ROUTES = new Map<string, PublicPage>([
  ['/(public)/user/[channel]', 'commands'],
  ['/(public)/[user]', 'leaderboard'],
  ['/(public)/queue/[user]', 'queue'],
  ['/(public)/sr/[user]', 'songqueue'],
  ['/(public)/stats', 'stats'],
  ['/(public)/login', 'login']
]);

export function publicPageForRoute(routeId: string | null): PublicPage | undefined {
  return PUBLIC_ROUTES.get(routeId ?? '');
}

export function usagePayload(input: BeaconEvent): string | null {
  const values: readonly string[] = input.event === 'public.views' ? PUBLIC_PAGES : input.event === 'marketing.cta' ? MARKETING_CTAS : [];
  if (!values.includes(input.dim)) return null;
  return JSON.stringify({ event: input.event, dim: input.dim });
}
