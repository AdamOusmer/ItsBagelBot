// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The guides registry: finds every content file, hands one to a page, and
 * refuses to build when two locales have drifted apart. The parity check runs
 * at module load (see the call at the bottom) so `astro build` and `astro dev`
 * fail loudly instead of shipping a French guide that quietly lost a section.
 */
import { locales, defaultLang, localizePath, type Lang } from '../../i18n/ui';
import { guideSlugs, guideLocalizedPaths, type GuideSlug } from './slugs';
import type { Block, GuideContent, HubContent, Section } from './types';

export { guideSlugs, guideLocalizedPaths };
export type { GuideSlug };

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

function lookup(key: string): GuideContent | HubContent | undefined {
  return Object.prototype.hasOwnProperty.call(content, key) ? content[key] : undefined;
}

/** One guide in one locale. Falls back to English so a half-translated locale still renders. */
export function getGuide(slug: string, lang: Lang): GuideContent {
  const found = lookup(`${slug}.${lang}`) ?? lookup(`${slug}.${defaultLang}`);
  if (!found) throw new Error(`guides registry: no content for "${slug}" in any locale`);
  return found as GuideContent;
}

/** The hub page in one locale, same English fallback. */
export function getHub(lang: Lang): HubContent {
  const found = lookup(`hub.${lang}`) ?? lookup(`hub.${defaultLang}`);
  if (!found) throw new Error('guides registry: no hub content in any locale');
  return found as HubContent;
}

/** The locale-aware URL of a guide, e.g. ('commands', 'fr') -> '/fr/guides/commands'. */
export function guidePath(slug: string, lang: Lang): string {
  return localizePath(`/guides/${slug}`, lang);
}

/** The locale-aware URL of the hub. */
export function hubPath(lang: Lang): string {
  return localizePath('/guides', lang);
}

/** The guide before this one in reading order, or undefined for the first. */
export function prevSlug(slug: string): GuideSlug | undefined {
  const i = (guideSlugs as readonly string[]).indexOf(slug);
  return i > 0 ? guideSlugs[i - 1] : undefined;
}

/** The guide after this one in reading order, or undefined for the last. */
export function nextSlug(slug: string): GuideSlug | undefined {
  const i = (guideSlugs as readonly string[]).indexOf(slug);
  return i >= 0 && i < guideSlugs.length - 1 ? guideSlugs[i + 1] : undefined;
}

/** The "01".."04" chapter number, from the slug's position in guideSlugs. */
export function guideIndex(slug: string): string {
  return String((guideSlugs as readonly string[]).indexOf(slug) + 1).padStart(2, '0');
}

// ── Parity ──────────────────────────────────────────────────────────────────

// Written as an escape, not the character: the repo greps its own sources for a
// literal em dash and this guard would be the one false positive.
const EM_DASH = '\u2014';

function fail(slug: string, lang: Lang, where: string, reason: string): never {
  throw new Error(`guides parity: ${slug} ${lang} ${where} ${reason}`);
}

/** Walk every string in a value and report the first that carries an em dash. */
function findEmDash(value: unknown, trail: string): string | undefined {
  if (typeof value === 'string') return value.includes(EM_DASH) ? trail : undefined;
  if (Array.isArray(value)) {
    for (let i = 0; i < value.length; i += 1) {
      const hit = findEmDash(value[i], `${trail}[${i}]`);
      if (hit) return hit;
    }
    return undefined;
  }
  if (value && typeof value === 'object') {
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      const hit = findEmDash(v, trail ? `${trail}.${k}` : k);
      if (hit) return hit;
    }
  }
  return undefined;
}

function assertBlockParity(slug: string, lang: Lang, sectionId: string, i: number, a: Block, b: Block): void {
  const where = `${sectionId} block ${i}`;
  if (a.kind !== b.kind) fail(slug, lang, where, `kind is "${b.kind}", English has "${a.kind}"`);
  if (a.kind === 'table' && b.kind === 'table') {
    if (a.head.length !== b.head.length) fail(slug, lang, where, `table has ${b.head.length} columns, English has ${a.head.length}`);
    if (a.rows.length !== b.rows.length) fail(slug, lang, where, `table has ${b.rows.length} rows, English has ${a.rows.length}`);
    for (let r = 0; r < a.rows.length; r += 1) {
      if (a.rows[r].length !== b.rows[r].length) fail(slug, lang, where, `table row ${r} has ${b.rows[r].length} cells, English has ${a.rows[r].length}`);
    }
  }
  if (a.kind === 'dash' && b.kind === 'dash') {
    if (a.screen !== b.screen) fail(slug, lang, where, `screen is "${b.screen}", English has "${a.screen}"`);
    if ((a.notes?.length ?? 0) !== (b.notes?.length ?? 0)) fail(slug, lang, where, `has ${b.notes?.length ?? 0} notes, English has ${a.notes?.length ?? 0}`);
  }
  if (a.kind === 'chat' && b.kind === 'chat') {
    if (a.lines.length !== b.lines.length) fail(slug, lang, where, `has ${b.lines.length} chat lines, English has ${a.lines.length}`);
    for (let l = 0; l < a.lines.length; l += 1) {
      if (a.lines[l].who !== b.lines[l].who) fail(slug, lang, where, `chat line ${l} is "${b.lines[l].who}", English has "${a.lines[l].who}"`);
    }
  }
  if (a.kind === 'widget' && b.kind === 'widget' && a.name !== b.name) {
    fail(slug, lang, where, `widget is "${b.name}", English has "${a.name}"`);
  }
  if (a.kind === 'cards' && b.kind === 'cards' && a.items.length !== b.items.length) {
    fail(slug, lang, where, `has ${b.items.length} cards, English has ${a.items.length}`);
  }
  if (a.kind === 'steps' && b.kind === 'steps' && a.items.length !== b.items.length) {
    fail(slug, lang, where, `has ${b.items.length} steps, English has ${a.items.length}`);
  }
}

function assertSectionParity(slug: string, lang: Lang, a: Section[], b: Section[]): void {
  if (a.length !== b.length) fail(slug, lang, 'sections', `has ${b.length} sections, English has ${a.length}`);
  for (let s = 0; s < a.length; s += 1) {
    if (a[s].id !== b[s].id) fail(slug, lang, `section ${s}`, `id is "${b[s].id}", English has "${a[s].id}"`);
    const blocksA = a[s].blocks;
    const blocksB = b[s].blocks;
    if (blocksA.length !== blocksB.length) fail(slug, lang, a[s].id, `has ${blocksB.length} blocks, English has ${blocksA.length}`);
    for (let i = 0; i < blocksA.length; i += 1) assertBlockParity(slug, lang, a[s].id, i, blocksA[i], blocksB[i]);
  }
}

/**
 * Every guide, every locale: same sections in the same order, same block shapes,
 * and no em dash anywhere. Called once at module load, so a mismatch stops the
 * build instead of reaching a reader.
 */
export function assertGuideParity(): void {
  for (const slug of ['hub', ...guideSlugs]) {
    const reference = lookup(`${slug}.${defaultLang}`);
    if (!reference) throw new Error(`guides parity: ${slug} ${defaultLang} file missing`);
    for (const lang of locales) {
      const candidate = lookup(`${slug}.${lang}`);
      if (!candidate) fail(slug, lang, 'file', 'missing');
      const emDash = findEmDash(candidate, '');
      if (emDash) fail(slug, lang, emDash || 'content', 'contains an em dash');
      if (lang === defaultLang || slug === 'hub') continue;
      assertSectionParity(slug, lang, (reference as GuideContent).sections, (candidate as GuideContent).sections);
    }
  }
}

assertGuideParity();
