// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The guides registry: finds the content files, hands one guide to a page, and
 * refuses to build when a locale has drifted from English. The parity guard
 * runs at module load (the call at the bottom), so nothing renders from a
 * drifted locale.
 */
import { defaultLang, localizePath, type Lang } from '../../i18n/ui';
import { guideSlugs, guideLocalizedPaths, isGuideSlug, type GuideSlug } from './slugs';
import { translate, type GuideStrings } from './translate';
import { assertGuideParity } from './parity';
import type { GuideContent, HubContent } from './types';

export { guideSlugs, guideLocalizedPaths, isGuideSlug };
export type { GuideSlug };

/**
 * Eager glob: every content file, bundled at build time and re-keyed from its
 * module path to '<slug>.<lang>'. A guide's English file is the guide itself
 * (a GuideContent); every other locale, and both hub files, are the other two
 * shapes this map holds, so callers below narrow it.
 */
const files = import.meta.glob<GuideContent | GuideStrings | HubContent>(
  '../../content/guides/*.ts',
  { eager: true, import: 'default' },
);

const content: Record<string, unknown> = {};
for (const path in files) {
  content[path.slice(path.lastIndexOf('/') + 1, -'.ts'.length)] = files[path];
}

function lookup(slug: string, lang: Lang): unknown {
  const key = `${slug}.${lang}`;
  return Object.prototype.hasOwnProperty.call(content, key) ? content[key] : undefined;
}

/** The English structure of one guide. Missing means the file was never written. */
export function englishGuide(slug: GuideSlug): GuideContent {
  const found = lookup(slug, defaultLang);
  if (!found) throw new Error(`guides registry: no ${defaultLang} copy for "${slug}"`);
  return found as GuideContent;
}

/** One locale's copy for one guide, or undefined when that locale has no file. */
export function guideStrings(slug: GuideSlug, lang: Lang): GuideStrings | undefined {
  return lang === defaultLang ? undefined : (lookup(slug, lang) as GuideStrings | undefined);
}

/** A slug straight off the URL, checked against the list the pages are built from. */
export function toGuideSlug(value: string): GuideSlug {
  if (!isGuideSlug(value)) throw new Error(`guides registry: "${value}" is not a guide`);
  return value;
}

/** One guide in one locale: the English guide with that locale's copy over it. */
export function getGuide(slug: GuideSlug, lang: Lang): GuideContent {
  const english = englishGuide(slug);
  const strings = guideStrings(slug, lang);
  return strings ? translate(english, strings) : english;
}

/** The hub page in one locale, same English fallback. */
export function getHub(lang: Lang): HubContent {
  const found = lookup('hub', lang) ?? lookup('hub', defaultLang);
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

/** The "01".."05" chapter number, from the slug's position in guideSlugs. */
export function guideIndex(slug: GuideSlug): string {
  return String(guideSlugs.indexOf(slug) + 1).padStart(2, '0');
}

assertGuideParity({
  english: englishGuide,
  strings: guideStrings,
  hub: (lang) => lookup('hub', lang) as HubContent | undefined,
});
