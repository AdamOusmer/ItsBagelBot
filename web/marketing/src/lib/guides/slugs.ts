// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const guideSlugs = ['getting-started', 'commands', 'data-sources', 'modules', 'counters'] as const;

export type GuideSlug = (typeof guideSlugs)[number];

export function isGuideSlug(value: string): value is GuideSlug {
  return (guideSlugs as readonly string[]).includes(value);
}

export const guideLocalizedPaths: string[] = [
  '/guides',
  ...guideSlugs.map((slug) => `/guides/${slug}`),
];
