// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The build-time parity guard: every guide has the same sections, in the same
 * order, with the same block shapes in every locale, and no em dash anywhere.
 * The registry calls it at module load, so `astro build` and `astro dev` fail
 * loudly instead of shipping a French guide that quietly lost a section.
 */
import { locales, defaultLang, type Lang } from '../../i18n/ui';
import { guideSlugs } from './slugs';
import { contentKey, lookup } from './content';
import type { Block, GuideContent, HubContent, Section } from './types';

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
  const candidate = lookup(contentKey(slug, lang));
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
    const reference = lookup(contentKey(slug, defaultLang));
    if (!reference) throw new Error(`guides parity: ${slug} ${defaultLang} file missing`);
    for (const lang of locales) assertLocaleParity(slug, lang, reference);
  }
}
