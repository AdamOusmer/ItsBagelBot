// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The build-time guard on guide copy. The registry calls it at module load, so
 * `astro build` and `astro dev` fail loudly rather than shipping a broken guide.
 *
 * It used to compare two whole locales against each other section by section
 * and block by block, because each locale carried its own copy of the guide's
 * structure and could lose a section or change a block kind. That cannot happen
 * now: the structure is one skeleton per guide (lib/guides/skeletons), and a
 * locale file is a flat map of strings. So the checks that remain are the ones
 * that still describe a way to be wrong:
 *
 *  - a key in a locale file that the skeleton does not name. That is a typo, a
 *    rename that only reached one language, or a line left behind by a deleted
 *    block, and it renders as nothing at all with no other symptom.
 *  - a guide with no English copy, since English is every other locale's
 *    per-key fallback.
 *  - an em dash anywhere, in a guide or in the hub.
 *
 * A key the skeleton names and a locale omits is NOT an error: the dashboard
 * mocks carry their own English labels, so an English guide legitimately
 * supplies no `labels` and a French one overrides every line of them.
 */
import { locales, defaultLang, type Lang } from '../../i18n/ui';
import { guideSlugs, type GuideSlug } from './slugs';
import { contentKey, lookup, skeleton, strings } from './content';
import { skeletonKeys } from './skeleton';

// Written as an escape, not the character: the repo greps its own sources for a
// literal em dash and this guard would be the one false positive.
const EM_DASH = '\u2014';

function fail(what: string, reason: string): never {
  throw new Error(`guides parity: ${what} ${reason}`);
}

/**
 * The children of a value paired with the trail that names each one: array
 * elements as `trail[i]`, object properties as `trail.key`, and nothing for a
 * leaf. Splitting this out keeps the walk below to a single loop rather than
 * one nested loop per container kind.
 */
function childEntries(value: unknown, trail: string): [unknown, string][] {
  if (Array.isArray(value)) return value.map((child, i) => [child, `${trail}[${i}]`]);
  if (value && typeof value === 'object') {
    return Object.entries(value as Record<string, unknown>).map(([k, child]) => [
      child,
      trail ? `${trail}.${k}` : k,
    ]);
  }
  return [];
}

/** Walk every string in a value and report the trail of the first em dash. */
function findEmDash(value: unknown, trail: string): string | undefined {
  if (typeof value === 'string') return value.includes(EM_DASH) ? trail : undefined;
  for (const [child, childTrail] of childEntries(value, trail)) {
    const hit = findEmDash(child, childTrail);
    if (hit !== undefined) return hit;
  }
  return undefined;
}

function assertNoEmDash(what: string, value: unknown): void {
  const hit = findEmDash(value, '');
  if (hit !== undefined) fail(what, `${hit || 'content'} contains an em dash`);
}

function assertGuideLocale(slug: GuideSlug, lang: Lang, named: ReadonlySet<string>): void {
  const copy = strings(slug, lang);
  if (!copy) return; // A locale with no file at all falls back to English wholesale.
  assertNoEmDash(`${slug} ${lang}`, copy);
  const orphan = Object.keys(copy).find((key) => !named.has(key));
  if (orphan) fail(`${slug} ${lang}`, `has key "${orphan}", which no skeleton entry names`);
}

function assertGuide(slug: GuideSlug): void {
  if (!strings(slug, defaultLang)) fail(`${slug} ${defaultLang}`, 'copy is missing');
  const named = new Set(skeletonKeys(skeleton(slug)));
  for (const lang of locales) assertGuideLocale(slug, lang, named);
}

function assertHubLocale(lang: Lang): void {
  const hub = lookup(contentKey('hub', lang));
  if (hub) return assertNoEmDash(`hub ${lang}`, hub);
  // A translated hub may be absent (it falls back to English wholesale); the
  // English one may not, since it is what everything else falls back to.
  if (lang === defaultLang) fail(`hub ${defaultLang}`, 'file is missing');
}

/** Every guide and the hub, every locale. Called once at module load. */
export function assertGuideParity(): void {
  for (const slug of guideSlugs) assertGuide(slug);
  for (const lang of locales) assertHubLocale(lang);
}
