// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { toRoadmap, type LinearProject } from './linear';
import { localizeRoadmap, staleItems } from './localize';

const NOW = new Date('2026-11-16T12:00:00Z');

function project(slugId: string, horizon: string | null, overrides: Partial<LinearProject> = {}): LinearProject {
  const labels: LinearProject['labels']['nodes'] = [{ name: 'Public', parent: null }];
  if (horizon) labels.push({ name: horizon, parent: { name: 'Horizon' } });
  return {
    slugId,
    name: `Title ${slugId}`,
    description: `Summary ${slugId}`,
    priority: 3,
    sortOrder: 0,
    completedAt: null,
    status: { type: 'backlog' },
    labels: { nodes: labels },
    ...overrides,
  };
}

test('projects land in their Horizon column, ordered by priority then manual order', () => {
  const roadmap = toRoadmap(
    [
      project('a', 'Now', { priority: 3 }),
      project('b', 'Now', { priority: 1 }),
      project('c', 'Now', { priority: 0 }),
      project('d', 'Now', { priority: 3, sortOrder: -5 }),
      project('e', 'Next'),
      project('f', 'Later'),
      project('g', null),
    ],
    NOW,
  );
  expect(Object.fromEntries(Object.entries(roadmap).map(([column, items]) => [column, items.map((i) => i.id)]))).toEqual({
    now: ['b', 'd', 'a', 'c'],
    next: ['e'],
    later: ['f'],
    shipped: [],
  });
});

test('completed projects show as shipped for 90 days, newest first; canceled ones never show', () => {
  const roadmap = toRoadmap(
    [
      project('old', 'Now', { status: { type: 'completed' }, completedAt: '2026-07-01T00:00:00Z' }),
      project('recent', 'Now', { status: { type: 'completed' }, completedAt: '2026-11-01T00:00:00Z' }),
      project('newest', 'Later', { status: { type: 'completed' }, completedAt: '2026-11-15T00:00:00Z' }),
      project('dropped', 'Next', { status: { type: 'canceled' } }),
    ],
    NOW,
  );
  expect([roadmap.shipped.map((i) => i.id), roadmap.now, roadmap.next]).toEqual([['newest', 'recent'], [], []]);
});

test('translations replace Linear copy per field, fall back to English, and flag stale English', () => {
  const roadmap = toRoadmap([project('a', 'Now'), project('b', 'Next')], NOW);
  const catalogs = {
    en: { a: { title: 'Title a', summary: 'Summary a' }, b: { title: 'Old title', summary: 'Summary b' } },
    fr: { a: { title: 'Titre a' } },
  };
  const fr = localizeRoadmap(roadmap, 'fr', catalogs);
  expect([fr.now[0], fr.next[0], staleItems(roadmap, catalogs)]).toEqual([
    { id: 'a', title: 'Titre a', summary: 'Summary a' },
    { id: 'b', title: 'Title b', summary: 'Summary b' },
    ['b'],
  ]);
});
