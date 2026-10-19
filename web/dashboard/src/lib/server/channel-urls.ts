// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { commandsHref, leaderboardHref } from '@bagel/kit/site-links';

const COMMANDS_ROUTE = '/(public)/user/[channel]';
const LEADERBOARD_ROUTE = '/(public)/[user]';

export const URL_PURGED_ROUTES: ReadonlySet<string> = new Set([COMMANDS_ROUTE, LEADERBOARD_ROUTE]);

// Root layout, (public) layout, page: SvelteKit sends one invalidation flag per node in __data.json requests.
export const CHANNEL_ROUTE_NODES = 3;

export const LEADERBOARD_CACHED_PAGES = 3;

function leafMasks(nodes: number): string[] {
  const layouts = nodes - 1;
  return Array.from({ length: 2 ** layouts }, (_, bits) => `${bits.toString(2).padStart(layouts, '0')}1`);
}

const MASKS = leafMasks(CHANNEL_ROUTE_NODES);

const LEADERBOARD_PAGE_QUERIES = ['', ...Array.from({ length: LEADERBOARD_CACHED_PAGES - 1 }, (_, i) => `page=${i + 2}`)];

function pageSearch(query: string): string {
  return query ? `?${query}` : '';
}

function dataSearches(query: string): string[] {
  return MASKS.map((mask) => `?${query ? `${query}&` : ''}x-sveltekit-invalidated=${mask}`);
}

function routeQueries(routeId: string): string[] {
  return routeId === LEADERBOARD_ROUTE ? LEADERBOARD_PAGE_QUERIES : [''];
}

const SHAREABLE: ReadonlyMap<string, { html: ReadonlySet<string>; data: ReadonlySet<string> }> = new Map(
  [...URL_PURGED_ROUTES].map((id) => [
    id,
    { html: new Set(routeQueries(id).map(pageSearch)), data: new Set(routeQueries(id).flatMap(dataSearches)) }
  ])
);

// Only URLs channelPageUrls enumerates may be shared, or a URL purge would leave copies behind.
export function enumerableChannelQuery(routeId: string, search: string, isDataRequest: boolean): boolean {
  const shareable = SHAREABLE.get(routeId);
  if (!shareable) return true;
  return (isDataRequest ? shareable.data : shareable.html).has(search);
}

function variants(page: string, queries: readonly string[]): string[] {
  return queries.flatMap((query) => [
    `${page}${pageSearch(query)}`,
    ...dataSearches(query).map((search) => `${page}/__data.json${search}`)
  ]);
}

export function channelPageUrls(userId: string | undefined, logins: readonly string[]): string[] {
  const segments = logins.flatMap((login) => [login, `@${login}`]);
  const commandsPages = [...segments.map(commandsHref), ...(userId ? [commandsHref(userId)] : [])];
  return [
    ...commandsPages.flatMap((page) => variants(page, routeQueries(COMMANDS_ROUTE))),
    ...segments.map(leaderboardHref).flatMap((page) => variants(page, routeQueries(LEADERBOARD_ROUTE)))
  ];
}
