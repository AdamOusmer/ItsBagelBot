// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The build-time guard on guide copy. The registry calls it at module load, so
 * `astro build` and `astro dev` fail loudly rather than shipping a broken guide.
 *
 * English is the structure, so a locale can no longer lose a section or change
 * a block kind; what it can still do is name an id that does not exist, or
 * miss one that does. Both are silent: an id nobody names renders as nothing,
 * and a missing one renders in English on a French page. So:
 *
 *  - every id a locale file carries must be one English names, or a `labels`
 *    entry on a block English does name. That exception is not a loophole, it
 *    is the point: the dashboard mocks under components/guides/screens carry
 *    their own English labels, so an English guide overrides none of them and
 *    a French one overrides every line.
 *  - every id English names must be in the locale file.
 *
 * A locale with no file for a guide at all is fine and falls back wholesale;
 * this checks files that exist.
 *
 * The em dash sweep this used to carry moved to lib/contentGuards.ts, which
 * runs it over all of src/content and the locale catalogs rather than over
 * guides alone.
 */
import { locales, defaultLang, type Lang } from '../../i18n/ui';
import { guideSlugs, type GuideSlug } from './slugs';
import { guideIds, type GuideStrings } from './translate';
import type { GuideContent, HubContent } from './types';

/** What the registry lends the guard, so the two need not import each other. */
export interface ParitySource {
  english(slug: GuideSlug): GuideContent;
  strings(slug: GuideSlug, lang: Lang): GuideStrings | undefined;
  hub(lang: Lang): HubContent | undefined;
}

function fail(what: string, reason: string): never {
  throw new Error(`guides parity: ${what} ${reason}`);
}

/** Whether an id adds a label to a block English names but supplies none for. */
function isBlockLabel(id: string, blocks: ReadonlySet<string>): boolean {
  const at = id.indexOf('.labels.');
  return at > 0 && blocks.has(id.slice(0, at));
}

function assertLocale(slug: GuideSlug, lang: Lang, copy: GuideStrings, english: GuideContent): void {
  const { ids, blocks } = guideIds(english);
  const named = new Set(ids);
  const blockIds = new Set(blocks);
  const orphan = Object.keys(copy).find((id) => !named.has(id) && !isBlockLabel(id, blockIds));
  if (orphan) fail(`${slug} ${lang}`, `has id "${orphan}", which the ${defaultLang} guide does not name`);
  const gap = ids.find((id) => !(id in copy));
  if (gap) fail(`${slug} ${lang}`, `is missing id "${gap}"`);
}

function assertGuide(slug: GuideSlug, source: ParitySource): void {
  const english = source.english(slug);
  for (const lang of locales) {
    const copy = source.strings(slug, lang);
    if (copy) assertLocale(slug, lang, copy, english);
  }
}

/** Every guide, every locale, plus the hub English falls back to. */
export function assertGuideParity(source: ParitySource): void {
  for (const slug of guideSlugs) assertGuide(slug, source);
  if (!source.hub(defaultLang)) fail(`hub ${defaultLang}`, 'file is missing');
}
