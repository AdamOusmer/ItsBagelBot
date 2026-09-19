// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Synchronous, glob-free counterpart to translate() (messages.ts). messages.ts
// resolves its non-default catalogs with a Vite import.meta.glob plus a lazy
// per-locale loader behind ensureCatalog() (a top-level await in every caller
// that needs a locale that isn't 'en'): neither import.meta.glob nor that
// await sequencing runs under bun. This file exists for the two callers that
// need kit copy resolved before any bundler runs at all: marketing's
// builder.ts (kitText), which Astro evaluates as a plain build-time module,
// and the marketing golden test (web/marketing/src/lib/variables/
// reference.golden.test.ts), which `bun test` evaluates directly with no
// Vite graph underneath it. Both need the same two catalogs kit already
// ships, just reached with a plain static `import`, which bun and Vite both
// support identically. messages.ts itself is untouched: the dashboard keeps
// its lazy per-locale loading.
import en from './locales/en.json';
import fr from './locales/fr.json';
import type { MessageTree } from './types';

const catalogs: Record<'en' | 'fr', MessageTree> = { en, fr };

// Mirrors lookup() in messages.ts exactly: string leaves only, a list or a
// missing branch is a miss, never a partial match.
type Node = string | string[] | MessageTree | undefined;

/** A branch is the only node kind a key can descend into. */
const isBranch = (node: Node): node is MessageTree => node != null && typeof node !== 'string' && !Array.isArray(node);

function lookup(tree: MessageTree | undefined, key: string): string | undefined {
  const node = key.split('.').reduce<Node>((current, part) => (isBranch(current) ? current[part] : undefined), tree);
  return typeof node === 'string' ? node : undefined;
}

/**
 * Resolve `key` for `locale` with no catalog-loading step: both catalogs are
 * already in memory via the static imports above. Falls back to English,
 * then to the key itself, the same miss semantics as translate() in
 * messages.ts, so a caller cannot tell which loader it got its string from.
 */
export function staticText(locale: 'en' | 'fr', key: string): string {
  return lookup(catalogs[locale], key) ?? lookup(catalogs.en, key) ?? key;
}
