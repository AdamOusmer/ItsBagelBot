// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { COMPARE_LOCALES } from '@bagel/kit/site-links';

export const compareSlugs = ['nightbot', 'streamelements'] as const;

export type CompareSlug = (typeof compareSlugs)[number];

export const compareLocales = COMPARE_LOCALES;

export const competitorNames: Record<CompareSlug, string> = {
  nightbot: 'Nightbot',
  streamelements: 'StreamElements',
};

export const compareLocalizedPaths: string[] = compareSlugs.map((slug) => `/vs/${slug}`);
