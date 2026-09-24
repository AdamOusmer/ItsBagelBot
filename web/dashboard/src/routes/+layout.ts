// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { ensureCatalog, isLocale } from '@bagel/kit/i18n';
import type { LayoutLoad } from './$types';

export const load: LayoutLoad = async ({ data }) => {
  await ensureCatalog(isLocale(data.locale) ? data.locale : 'en');
  return data;
};
