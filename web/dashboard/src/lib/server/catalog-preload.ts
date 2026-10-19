// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { DEFAULT_LOCALE } from '@bagel/kit/i18n';
import { sliced, urls } from 'virtual:bagel-i18n';

const CHARSET_META = '<meta charset="utf-8" />';
const NODE_CHUNK = /<\/_app\/immutable\/nodes\/([\w.-]+)\.js>/g;

export function preloadCatalog(html: string, locale: string): string {
  const href = sliced ? undefined : urls[locale];
  if (!href || !html.includes(CHARSET_META)) return html;
  return html.replace(CHARSET_META, `${CHARSET_META}<link rel="preload" as="fetch" crossorigin="anonymous" href="${href}" />`);
}

export function preloadSlices(headers: Headers, locale: string): void {
  const link = sliced && locale !== DEFAULT_LOCALE ? headers.get('link') : null;
  if (!link) return;
  const nodes = new Set([...link.matchAll(NODE_CHUNK)].map((match) => match[1]));
  const slices = [...nodes].map((node) => `</_app/immutable/i18n/${locale}/${node}.json>; rel="preload"; as="fetch"; crossorigin="anonymous"; nopush`);
  if (slices.length) headers.set('link', [link, ...slices].join(', '));
}
