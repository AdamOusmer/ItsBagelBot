// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { locales, defaultLang, type Lang } from '../../i18n/ui';
import { guideSlugs, type GuideSlug } from './slugs';
import { guideIds, type GuideStrings } from './translate';
import type { GuideContent, HubContent } from './types';

export interface ParitySource {
  english(slug: GuideSlug): GuideContent;
  strings(slug: GuideSlug, lang: Lang): GuideStrings | undefined;
  hub(lang: Lang): HubContent | undefined;
}

function fail(what: string, reason: string): never {
  throw new Error(`guides parity: ${what} ${reason}`);
}

function isBlockLabel(id: string, blocks: ReadonlySet<string>): boolean {
  const at = id.indexOf('.labels.');
  return at > 0 && blocks.has(id.slice(0, at));
}

function assertLocale(slug: GuideSlug, lang: Lang, copy: GuideStrings, english: GuideContent): void {
  if (!isStringMap(copy)) {
    fail(`${slug} ${lang}`, 'must be a JSON object of string values');
  }
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

export function assertGuideParity(source: ParitySource): void {
  for (const slug of guideSlugs) assertGuide(slug, source);
  if (!source.hub(defaultLang)) fail(`hub ${defaultLang}`, 'file is missing');
}

function isStringMap(value: unknown): value is GuideStrings {
  if (!value) return false;
  if (typeof value !== 'object') return false;
  if (Array.isArray(value)) return false;
  return Object.values(value).every((entry) => typeof entry === 'string');
}
