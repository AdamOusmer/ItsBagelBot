// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { commandsHref } from '@bagel/kit/site-links';

const COMMANDS_ROUTE = '/(public)/user/[channel]';

export const URL_PURGED_ROUTES: ReadonlySet<string> = new Set([COMMANDS_ROUTE]);

// Root layout, (public) layout, page: SvelteKit sends one invalidation flag per node in __data.json requests.
export const CHANNEL_ROUTE_NODES = 3;

function leafMasks(nodes: number): string[] {
  const layouts = nodes - 1;
  return Array.from({ length: 2 ** layouts }, (_, bits) => `${bits.toString(2).padStart(layouts, '0')}1`);
}

const DATA_QUERIES = leafMasks(CHANNEL_ROUTE_NODES).map((mask) => `?x-sveltekit-invalidated=${mask}`);

const CACHEABLE_DATA_QUERIES: ReadonlySet<string> = new Set(DATA_QUERIES);

// Only URLs channelPageUrls enumerates may be shared on these routes, or a URL purge would leave copies behind.
export function enumerableChannelQuery(routeId: string, search: string, isDataRequest: boolean): boolean {
  if (!URL_PURGED_ROUTES.has(routeId)) return true;
  return isDataRequest ? CACHEABLE_DATA_QUERIES.has(search) : search === '';
}

function withData(page: string): string[] {
  return [page, ...DATA_QUERIES.map((query) => `${page}/__data.json${query}`)];
}

export function channelPageUrls(userId: string | undefined, logins: readonly string[]): string[] {
  const segments = logins.flatMap((login) => [login, `@${login}`]);
  const pages = [...segments.map(commandsHref), ...(userId ? [commandsHref(userId)] : [])];
  return pages.flatMap(withData);
}
