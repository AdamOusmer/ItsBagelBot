// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The guides registry: hands one content file to a page, and refuses to build
 * when two locales have drifted apart. The parity guard runs at module load
 * (see the call at the bottom), so nothing renders from a drifted locale.
 */
import { defaultLang, localizePath, type Lang } from '../../i18n/ui';
import { guideSlugs, guideLocalizedPaths, isGuideSlug, type GuideSlug } from './slugs';
import { contentKey, lookup } from './content';
import { assertGuideParity } from './parity';
import type { GuideContent, HubContent } from './types';

export { guideSlugs, guideLocalizedPaths, isGuideSlug };
export type { GuideSlug };

/** A slug straight off the URL, checked against the list the pages are built from. */
export function toGuideSlug(value: string): GuideSlug {
  if (!isGuideSlug(value)) throw new Error(`guides registry: "${value}" is not a guide`);
  return value;
}

/** One guide in one locale. Falls back to English so a half-translated locale still renders. */
export function getGuide(slug: GuideSlug, lang: Lang): GuideContent {
  const found = lookup(contentKey(slug, lang)) ?? lookup(contentKey(slug, defaultLang));
  if (!found) throw new Error(`guides registry: no content for "${slug}" in any locale`);
  return found as GuideContent;
}

/** The hub page in one locale, same English fallback. */
export function getHub(lang: Lang): HubContent {
  const found = lookup(contentKey('hub', lang)) ?? lookup(contentKey('hub', defaultLang));
  if (!found) throw new Error('guides registry: no hub content in any locale');
  return found as HubContent;
}

/** The locale-aware URL of a guide, e.g. ('commands', 'fr') -> '/fr/guides/commands'. */
export function guidePath(slug: GuideSlug, lang: Lang): string {
  return localizePath(`/guides/${slug}`, lang);
}

/** The locale-aware URL of the hub. */
export function hubPath(lang: Lang): string {
  return localizePath('/guides', lang);
}

/** The guide before this one in reading order, or undefined for the first. */
export function prevSlug(slug: GuideSlug): GuideSlug | undefined {
  const i = guideSlugs.indexOf(slug);
  return i > 0 ? guideSlugs[i - 1] : undefined;
}

/** The guide after this one in reading order, or undefined for the last. */
export function nextSlug(slug: GuideSlug): GuideSlug | undefined {
  const i = guideSlugs.indexOf(slug);
  return i >= 0 && i < guideSlugs.length - 1 ? guideSlugs[i + 1] : undefined;
}

/** The "01".."05" chapter number, from the slug's position in guideSlugs. */
export function guideIndex(slug: GuideSlug): string {
  return String(guideSlugs.indexOf(slug) + 1).padStart(2, '0');
}

assertGuideParity();
