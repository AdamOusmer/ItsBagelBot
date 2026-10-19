// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { urls } from 'virtual:bagel-i18n';

const CHARSET_META = '<meta charset="utf-8" />';

export function preloadCatalog(html: string, locale: string): string {
  const href = urls[locale];
  if (!href || !html.includes(CHARSET_META)) return html;
  return html.replace(CHARSET_META, `${CHARSET_META}<link rel="preload" as="fetch" crossorigin="anonymous" href="${href}" />`);
}
