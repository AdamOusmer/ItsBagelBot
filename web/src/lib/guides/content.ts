// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Where the guide content files are found and how they are keyed. Kept apart
 * from the registry's public accessors and from the parity guard so that both
 * can read the same map without importing each other.
 */
import type { Lang } from '../../i18n/ui';
import type { GuideContent, HubContent } from './types';

/** The key a content file is stored under: its slug, a dot, its locale. */
export type ContentKey = `${string}.${Lang}`;

export function contentKey(slug: string, lang: Lang): ContentKey {
  return `${slug}.${lang}`;
}

// Eager glob: every content file, bundled at build time. Keyed by module path
// ('../../content/guides/commands.fr.ts'), re-keyed below as '<slug>.<lang>'.
const files = import.meta.glob<GuideContent | HubContent>('../../content/guides/*.ts', {
  eager: true,
  import: 'default',
});

const content: Record<string, GuideContent | HubContent> = {};
for (const path in files) {
  const base = path.slice(path.lastIndexOf('/') + 1, -'.ts'.length);
  content[base] = files[path];
}

export function lookup(key: ContentKey): GuideContent | HubContent | undefined {
  return Object.prototype.hasOwnProperty.call(content, key) ? content[key] : undefined;
}
