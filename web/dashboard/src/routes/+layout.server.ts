// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { LOCALES, ensureCatalog, localeName } from '@bagel/kit/i18n';
import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = async ({ locals }) => {
  await Promise.all(LOCALES.map(ensureCatalog));
  return {
    locale: locals.locale,
    cursorEnabled: locals.cursorEnabled,
    localeNames: Object.fromEntries(LOCALES.map((code) => [code, localeName(code)])) as Record<string, string>
  };
};
