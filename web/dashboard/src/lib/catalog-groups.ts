// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const CATALOG_EXCLUDED = ['admin'];

export const CATALOG_GROUPS = {
  discord: ['discord'],
  integrations: ['spotify', 'spotifySetup', 'goveeSetup', 'setupAi'],
  import: ['import', 'onboardingImport'],
  editor: ['vars', 'replyVars', 'fetches']
};

type CatalogGroup = keyof typeof CATALOG_GROUPS;

const ROUTE_GROUPS: readonly (readonly [prefix: string, groups: readonly CatalogGroup[]])[] = [
  ['/(app)/discord', ['discord']],
  ['/(app)/songqueue', ['integrations', 'import', 'editor']],
  ['/(app)/govee', ['integrations', 'editor']],
  ['/(app)/commands', ['editor']],
  ['/(app)/timers', ['editor']],
  ['/(app)/channelpoints', ['editor']],
  ['/(app)/modules/[id]', ['editor']],
  ['/(app)/settings', ['editor']],
  ['/(app)/settings/import', ['import']],
  ['/welcome', ['import']]
];

export function groupsForRoute(routeId: string | null): CatalogGroup[] {
  if (!routeId) return [];
  const groups = new Set<CatalogGroup>();
  for (const [prefix, names] of ROUTE_GROUPS) {
    if (routeId.startsWith(prefix)) names.forEach((name) => groups.add(name));
  }
  return [...groups];
}
