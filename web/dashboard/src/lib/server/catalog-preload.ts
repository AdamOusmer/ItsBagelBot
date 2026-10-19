// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { urls } from 'virtual:bagel-i18n';
import { groupsForRoute } from '$lib/catalog-groups';

const CHARSET_META = '<meta charset="utf-8" />';

export function preloadCatalog(html: string, locale: string, routeId: string | null): string {
  if (!html.includes(CHARSET_META)) return html;
  const parts = ['core', ...groupsForRoute(routeId)];
  const tags = parts
    .map((part) => urls[locale]?.[part])
    .filter(Boolean)
    .map((href) => `<link rel="preload" as="fetch" crossorigin="anonymous" href="${href}" />`)
    .join('');
  return html.replace(CHARSET_META, CHARSET_META + tags);
}
