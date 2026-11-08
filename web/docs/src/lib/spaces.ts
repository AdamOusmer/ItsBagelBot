// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface SpaceGroup {
  label?: string;
  pages: readonly string[];
  prefix?: string;
}

export interface Space {
  key: string;
  label: string;
  featured: readonly string[];
  groups: readonly SpaceGroup[];
}

export const SPACES: readonly Space[] = [
  {
    key: 'start',
    label: 'space.start',
    featured: ['guides/getting-started', 'guides/testing', 'reference/sesame-module-builder'],
    groups: [
      { label: 'space.groupDevelopment', pages: ['guides/getting-started', 'guides/testing', 'reference/sesame-module-builder'] },
      { label: 'space.groupValidation', pages: ['infrastructure/observability', 'infrastructure/performance', 'qa', 'qa/outgress-hot-path-canary', 'qa/monkey', 'qa/ai-review'] },
    ],
  },
  {
    key: 'platform',
    label: 'space.platform',
    featured: ['architecture', 'data-and-state', 'data-and-state/projection', 'infrastructure/releases'],
    groups: [
      { pages: ['architecture'] },
      { label: 'data', pages: ['data-and-state', 'data-and-state/database', 'data-and-state/design', 'data-and-state/caching', 'data-and-state/projection'] },
      { pages: ['infrastructure/releases'] },
    ],
  },
  {
    key: 'services',
    label: 'services',
    featured: ['microservices', 'microservices/twitch-ingress', 'microservices/sesame', 'microservices/outgress'],
    groups: [
      { pages: ['microservices'] },
      { label: 'space.groupEvents', pages: ['microservices/twitch-ingress', 'microservices/sesame', 'microservices/outgress', 'microservices/dingress', 'microservices/discord-engine', 'microservices/discord-outgress', 'microservices/automod', 'microservices/projector'] },
      { label: 'space.groupData', pages: ['microservices/users', 'microservices/commands', 'microservices/modules', 'microservices/loyalty', 'microservices/discord-data', 'microservices/notifications', 'microservices/transactions'] },
      { label: 'space.groupWeb', pages: ['microservices/console', 'microservices/admin', 'microservices/web', 'microservices/controls-gateway', 'microservices/desktop'] },
      { label: 'space.groupRuntime', pages: ['microservices/gossip', 'microservices/warp'] },
    ],
  },
  {
    key: 'reference',
    label: 'reference',
    featured: ['reference/system-overview', 'reference/rpc-contracts', 'reference/shared-packages'],
    groups: [{ pages: ['reference/system-overview', 'reference/rpc-contracts', 'reference/module-variables', 'reference/shared-packages', 'infrastructure/runtime-configuration', 'reference/documentation-standards'] }],
  },
  {
    key: 'records',
    label: 'space.records',
    featured: ['adr'],
    groups: [{ label: 'adr', pages: ['adr'], prefix: 'adr/' }],
  },
];

/** Resolves every group to its page ids, in reading order; `prefix` pulls in numbered records sorted by name. */
export function resolvedSpaces(ids: readonly string[]): Space[] {
  const sorted = [...ids].sort();
  return SPACES.map((space) => ({
    ...space,
    groups: space.groups.map((group) => ({
      label: group.label,
      pages: [...group.pages, ...(group.prefix ? sorted.filter((id) => id.startsWith(group.prefix!)) : [])],
    })),
  }));
}

export function spaceOf(id: string, spaces: readonly Space[]): Space {
  const space = spaces.find(({ groups }) => groups.some(({ pages }) => pages.includes(id)));
  if (!space) throw new Error(`docs: page "${id}" belongs to no space, add it in src/lib/spaces.ts`);
  return space;
}

export function neighbours(id: string, space: Space): { prev?: string; next?: string } {
  const order = space.groups.flatMap(({ pages }) => pages);
  const at = order.indexOf(id);
  return { prev: order[at - 1], next: order[at + 1] };
}
