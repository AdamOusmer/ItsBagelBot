// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defaultLang, localizePath, type Lang } from '../../i18n/ui';
import { guideSlugs, guideLocalizedPaths, isGuideSlug, type GuideSlug } from './slugs';
import { translate, type GuideStrings } from './translate';
import { assertGuideParity } from './parity';
import type { GuideContent, HubContent } from './types';

export { guideSlugs, guideLocalizedPaths, isGuideSlug };
export type { GuideSlug };

const files = import.meta.glob<GuideContent | GuideStrings | HubContent>(
  ['../../content/guides/*.en.ts', '../../content/guides/*.json'],
  { eager: true, import: 'default' },
);

const content: Record<string, unknown> = {};
for (const path in files) {
  content[path.slice(path.lastIndexOf('/') + 1).replace(/\.(?:ts|json)$/, '')] = files[path];
}

function lookup(slug: string, lang: Lang): unknown {
  const key = `${slug}.${lang}`;
  return Object.prototype.hasOwnProperty.call(content, key) ? content[key] : undefined;
}

export function englishGuide(slug: GuideSlug): GuideContent {
  const found = lookup(slug, defaultLang);
  if (!found) throw new Error(`guides registry: no ${defaultLang} copy for "${slug}"`);
  return found as GuideContent;
}

export function guideStrings(slug: GuideSlug, lang: Lang): GuideStrings | undefined {
  return lang === defaultLang ? undefined : (lookup(slug, lang) as GuideStrings | undefined);
}

export function toGuideSlug(value: string): GuideSlug {
  if (!isGuideSlug(value)) throw new Error(`guides registry: "${value}" is not a guide`);
  return value;
}

export function getGuide(slug: GuideSlug, lang: Lang): GuideContent {
  const english = englishGuide(slug);
  const strings = guideStrings(slug, lang);
  return strings ? translate(english, strings) : english;
}

export function getHub(lang: Lang): HubContent {
  const found = lookup('hub', lang) ?? lookup('hub', defaultLang);
  if (!found) throw new Error('guides registry: no hub content in any locale');
  return found as HubContent;
}

export function guidePath(slug: GuideSlug, lang: Lang): string {
  return localizePath(`/guides/${slug}`, lang);
}

export function hubPath(lang: Lang): string {
  return localizePath('/guides', lang);
}

export function guideIndex(slug: GuideSlug): string {
  return String(guideSlugs.indexOf(slug) + 1).padStart(2, '0');
}

assertGuideParity({
  english: englishGuide,
  strings: guideStrings,
  hub: (lang) => lookup('hub', lang) as HubContent | undefined,
});
