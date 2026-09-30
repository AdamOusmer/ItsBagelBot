// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createUiI18n, type UiI18n } from '../lib/i18n';

interface AstroLike {
  currentLocale?: string | undefined;
  locals?: object;
}

export function uiI18n(astro: AstroLike): UiI18n {
  return createUiI18n(() => {
    const chosen = (astro.locals as { uiLocale?: unknown } | undefined)?.uiLocale;
    return typeof chosen === 'string' ? chosen : astro.currentLocale;
  });
}
