// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Roadmap, RoadmapItem } from './linear';

type ItemCopy = Partial<Pick<RoadmapItem, 'title' | 'summary'>>;
export type ItemCatalogs = Record<string, Record<string, ItemCopy>>;

export function localizeRoadmap(roadmap: Roadmap, lang: string, catalogs: ItemCatalogs): Roadmap {
  if (lang === 'en') return roadmap;
  const table = catalogs[lang] ?? {};
  const localize = (item: RoadmapItem): RoadmapItem => ({
    id: item.id,
    title: table[item.id]?.title ?? item.title,
    summary: table[item.id]?.summary ?? item.summary,
  });
  return {
    now: roadmap.now.map(localize),
    next: roadmap.next.map(localize),
    later: roadmap.later.map(localize),
    shipped: roadmap.shipped.map(localize),
  };
}

export function staleItems(roadmap: Roadmap, catalogs: ItemCatalogs): string[] {
  const english = catalogs.en ?? {};
  return Object.values(roadmap)
    .flat()
    .filter((item) => english[item.id]?.title !== item.title || english[item.id]?.summary !== item.summary)
    .map((item) => item.id);
}
