// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Where the guide content is found and how it is keyed. Kept apart from the
 * registry's public accessors and from the parity guard so that both can read
 * the same maps without importing each other.
 *
 * Two globs, because a guide is now two things: one skeleton per slug (its
 * structure) and one string map per slug per locale (its copy). The hub is
 * still a whole content object per locale: it is one screenful of links with
 * no block structure to share, so splitting it would cost a file and buy
 * nothing.
 */
import type { Lang } from '../../i18n/ui';
import type { GuideSkeleton, GuideStrings } from './skeleton';
import type { GuideSlug } from './slugs';
import type { HubContent } from './types';

/** The key a locale's copy is stored under: its slug, a dot, its locale. */
export type ContentKey = `${string}.${Lang}`;

export function contentKey(slug: string, lang: Lang): ContentKey {
  return `${slug}.${lang}`;
}

// Eager globs: every content file and every skeleton, bundled at build time.
// Keyed by module path, re-keyed below as '<slug>.<lang>' and '<slug>'.
const files = import.meta.glob<GuideStrings | HubContent>('../../content/guides/*.ts', {
  eager: true,
  import: 'default',
});

const skeletonFiles = import.meta.glob<GuideSkeleton>('./skeletons/*.ts', {
  eager: true,
  import: 'default',
});

function basename(path: string, ext: string): string {
  return path.slice(path.lastIndexOf('/') + 1, -ext.length);
}

const content: Record<string, GuideStrings | HubContent> = {};
for (const path in files) content[basename(path, '.ts')] = files[path];

const skeletons: Record<string, GuideSkeleton> = {};
for (const path in skeletonFiles) skeletons[basename(path, '.ts')] = skeletonFiles[path];

export function lookup(key: ContentKey): GuideStrings | HubContent | undefined {
  return Object.prototype.hasOwnProperty.call(content, key) ? content[key] : undefined;
}

/** One locale's copy for one guide, or undefined when that locale has no file. */
export function strings(slug: GuideSlug, lang: Lang): GuideStrings | undefined {
  return lookup(contentKey(slug, lang)) as GuideStrings | undefined;
}

/** A guide's structure. Missing means the skeleton file was never written. */
export function skeleton(slug: GuideSlug): GuideSkeleton {
  const found = skeletons[slug];
  if (!found) throw new Error(`guides: no skeleton for "${slug}"`);
  return found;
}
