// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Universal root load: runs on the server AND on the client before the tree
// renders (including during hydration), which is exactly the guarantee the lazy
// i18n catalogs need — by the time any component translates a string, the
// active locale's catalog chunk is registered, so client output matches SSR
// byte-for-byte. English is bundled eagerly; every other locale pays one small
// parallel JSON-chunk fetch here instead of ~54 KB of eager eval for every
// visitor at boot.
import { ensureCatalog, isLocale } from '@bagel/shared/i18n';
import type { LayoutLoad } from './$types';

export const load: LayoutLoad = async ({ data }) => {
  await ensureCatalog(isLocale(data.locale) ? data.locale : 'en');
  return data;
};
