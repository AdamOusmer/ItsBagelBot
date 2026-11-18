// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ItemCatalogs } from './localize';

const files = import.meta.glob<ItemCatalogs[string]>('../../../../../locales/*/roadmap/items.json', {
  eager: true,
  import: 'default',
});

export const itemCatalogs: ItemCatalogs = Object.fromEntries(
  Object.entries(files).map(([path, items]) => [path.split('/').at(-3) ?? '', items]),
);
