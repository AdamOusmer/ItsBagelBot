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

/**
 * The slug and locale under test, bound once. Every check below reports through
 * this instead of threading the pair (and the section label) through each call:
 * passing them as loose strings is what made the block checker a six-argument
 * function that no caller could read.
 */
interface Parity {
  fail(where: string, reason: string): never;
}

function parityFor(slug: string, lang: Lang): Parity {
  return {
    fail(where: string, reason: string): never {
      throw new Error(`guides parity: ${slug} ${lang} ${where} ${reason}`);
    },
  };
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

/** A drift report, or undefined when the two sides agree. */
type Drift = string | undefined;

type BlockOf<K extends Block['kind']> = Extract<Block, { kind: K }>;

/**
 * One comparator per block kind, each returning what drifted rather than
 * throwing. Written as a table because the alternative -- one `if (a.kind ===
 * 'x' && b.kind === 'x')` arm per kind in a single function -- re-narrows both
 * sides on every arm and grows a branch with every kind the content model gains.
 */
type BlockChecks = { [K in Block['kind']]?: (a: BlockOf<K>, b: BlockOf<K>) => Drift };

function countDrift(label: string, a: readonly unknown[], b: readonly unknown[]): Drift {
  return a.length === b.length ? undefined : `has ${b.length} ${label}, English has ${a.length}`;
}

function firstDefined(drifts: readonly Drift[]): Drift {
  return drifts.find((drift) => drift !== undefined);
}

const blockChecks: BlockChecks = {
  table: (a, b) =>
    countDrift('table columns', a.head, b.head) ??
    countDrift('table rows', a.rows, b.rows) ??
    firstDefined(a.rows.map((row, r) => countDrift(`cells in table row ${r}`, row, b.rows[r]))),
  dash: (a, b) =>
    (a.screen === b.screen ? undefined : `screen is "${b.screen}", English has "${a.screen}"`) ??
    countDrift('notes', a.notes ?? [], b.notes ?? []),
  chat: (a, b) =>
    countDrift('chat lines', a.lines, b.lines) ??
    firstDefined(
      a.lines.map((line, l) =>
        line.who === b.lines[l].who
          ? undefined
          : `chat line ${l} is "${b.lines[l].who}", English has "${line.who}"`,
      ),
    ),
  widget: (a, b) => (a.name === b.name ? undefined : `widget is "${b.name}", English has "${a.name}"`),
  cards: (a, b) => countDrift('cards', a.items, b.items),
  steps: (a, b) => countDrift('steps', a.items, b.items),
};

function blockDrift(a: Block, b: Block): Drift {
  // The table keys off the kind the pair already share, so the two sides are the
  // same variant by construction; the cast is what the index signature cannot say.
  const check = blockChecks[a.kind] as ((x: Block, y: Block) => Drift) | undefined;
  return check?.(a, b);
}

function assertBlockParity(p: Parity, where: string, a: Block, b: Block): void {
  if (a.kind !== b.kind) p.fail(where, `kind is "${b.kind}", English has "${a.kind}"`);
  const drift = blockDrift(a, b);
  if (drift) p.fail(where, drift);
}

function assertSectionParity(p: Parity, a: Section[], b: Section[]): void {
  const count = countDrift('sections', a, b);
  if (count) p.fail('sections', count);
  for (let s = 0; s < a.length; s += 1) assertOneSection(p, s, a[s], b[s]);
}

function assertOneSection(p: Parity, s: number, a: Section, b: Section): void {
  if (a.id !== b.id) p.fail(`section ${s}`, `id is "${b.id}", English has "${a.id}"`);
  const count = countDrift('blocks', a.blocks, b.blocks);
  if (count) p.fail(a.id, count);
  for (let i = 0; i < a.blocks.length; i += 1) {
    assertBlockParity(p, `${a.id} block ${i}`, a.blocks[i], b.blocks[i]);
  }
}

function assertLocaleParity(slug: string, lang: Lang, reference: GuideContent | HubContent): void {
  const p = parityFor(slug, lang);
  const candidate = lookup(`${slug}.${lang}`);
  if (!candidate) p.fail('file', 'missing');
  const emDash = findEmDash(candidate, '');
  if (emDash !== undefined) p.fail(emDash || 'content', 'contains an em dash');
  // The hub has no sections, and English is the reference it would be compared to.
  if (lang === defaultLang || slug === 'hub') return;
  assertSectionParity(p, (reference as GuideContent).sections, (candidate as GuideContent).sections);
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
    for (const lang of locales) assertLocaleParity(slug, lang, reference);
  }
}

assertGuideParity();
