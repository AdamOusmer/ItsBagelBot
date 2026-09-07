// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The guide slugs, and nothing else. This file has no imports on purpose:
 * i18n/ui.ts needs the guide paths for LOCALIZED_PATHS and registry.ts needs
 * locales/localizePath from i18n/ui.ts, so the slug list lives here where both
 * can read it without the two modules importing each other.
 *
 * Order is meaningful: it is the hub card order, the "01".."04" chapter
 * numbers, and the prev/next pager order.
 */
export const guideSlugs = ['getting-started', 'commands', 'modules', 'counters'] as const;

export type GuideSlug = (typeof guideSlugs)[number];

/** English paths of every guide page, hub first. Feeds LOCALIZED_PATHS. */
export const guideLocalizedPaths: string[] = [
  '/guides',
  ...guideSlugs.map((slug) => `/guides/${slug}`),
];
