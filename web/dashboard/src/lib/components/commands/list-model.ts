// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CommandView, Perm } from '@bagel/kit';
import { compareUses } from '../../../../../kit/lib/uses';

export const STATE_FILTERS = ['all', 'active', 'disabled', 'builtin', 'custom'] as const;
export type StateFilter = (typeof STATE_FILTERS)[number];
export type SortKey = 'uses' | 'name' | 'recent';
export type PermFilter = Perm | 'all';

export interface ListQuery {
  state: StateFilter;
  perm: PermFilter;
  sort: SortKey;
  search: string;
  keep?: ReadonlySet<string>;
}

const STATE_TESTS: Record<StateFilter, (c: CommandView) => boolean> = {
  all: () => true,
  active: (c) => c.is_active,
  disabled: (c) => !c.is_active,
  builtin: (c) => c.builtin === true,
  custom: (c) => c.builtin !== true
};

function matchesSearch(c: CommandView, search: string): boolean {
  const q = search.toLowerCase();
  return (
    c.name.toLowerCase().includes(q) ||
    (c.aliases ?? []).some((a) => a.toLowerCase().includes(q)) ||
    c.response.toLowerCase().includes(q)
  );
}

const matchesPerm = (c: CommandView, perm: PermFilter) => perm === 'all' || (c.perm ?? 'everyone') === perm;

export function stateCounts(items: CommandView[], q: Pick<ListQuery, 'perm' | 'search'>): Record<StateFilter, number> {
  const scoped = items.filter((c) => matchesPerm(c, q.perm) && matchesSearch(c, q.search));
  return Object.fromEntries(
    STATE_FILTERS.map((f) => [f, scoped.filter(STATE_TESTS[f]).length])
  ) as Record<StateFilter, number>;
}

const SORTERS: Record<SortKey, (a: CommandView, b: CommandView) => number> = {
  uses: (a, b) => compareUses(b, a) || a.name.localeCompare(b.name),
  name: (a, b) => a.name.localeCompare(b.name),
  recent: (a, b) => (b.created_at ?? 0) - (a.created_at ?? 0) || a.name.localeCompare(b.name)
};

export const hasCreatedAt = (items: CommandView[]) => items.some((c) => c.created_at !== undefined);

export function listCommands(items: CommandView[], q: ListQuery): CommandView[] {
  return items
    .filter((c) => STATE_TESTS[q.state](c) || q.keep?.has(c.name) === true)
    .filter((c) => matchesPerm(c, q.perm) && matchesSearch(c, q.search))
    .toSorted(SORTERS[q.sort]);
}
