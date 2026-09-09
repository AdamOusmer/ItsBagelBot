// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * How a guide gets translated: the English file is the guide, and every other
 * locale is a flat map of ids to text laid over it.
 *
 * Why this shape. A guide's structure (section order and ids, block kinds,
 * which mock screen a block shows, who speaks each chat line) is not copy, and
 * writing it out again per language was ~230 lines of duplication per guide per
 * locale kept honest only by a parity guard. The fix does not need a separate
 * structure file with typed placeholders in it: English is already the
 * structure, and an id can be *derived* from where a string sits rather than
 * hand-written next to it. So there is one file per guide (en, plain
 * GuideContent, fully typed and readable as the guide it is) plus one flat
 * string map per other locale.
 *
 * Rejected: a `Skeleton<GuideContent>` type with `k('id')` markers in a third
 * file per guide (shipped in #836, reverted here). It bought the same
 * de-duplication and cost a Key class, a recursive mapped type, a hydrate
 * walker and five skeleton files: 1602 lines added to remove 1296, with the
 * payoff only arriving at a third locale. Deriving the id from the path costs
 * one walk and no new type.
 *
 * The ids are the same strings #836 wrote by hand, verified key-for-key
 * against all five guides before that layer was deleted.
 */
import type { GuideContent } from './types';

/** One locale's copy for one guide: every id the English structure names. */
export type GuideStrings = Record<string, string>;

/**
 * String-valued fields that are structure, not copy, and so get no id: they
 * name a section anchor, pick a block's renderer or its mock screen, say which
 * side of a chat a line sits on, or carry widget data (a localStorage key, a
 * sample JSON payload) that a translator must not touch. `name` is only
 * literal on a widget block, where it picks the component; on a chat line it
 * is the speaker's display name and is copy.
 */
const LITERAL = new Set(['slug', 'id', 'kind', 'tone', 'screen', 'path', 'who', 'storageKey', 'sample']);

/** Where a value lives, as a dotted id: `tour.b1.labels.account`. */
interface Found {
    /** Ids of every translatable string, in traversal order. */
    ids: string[];
    /** Ids of every block, e.g. `tour.b1`. Lets the parity guard resolve a
     *  key that adds a label the English mock supplies itself. */
    blocks: string[];
}

/** Every id an English guide names, plus the block ids fr may extend. */
export function guideIds(guide: GuideContent): Found {
    const found: Found = { ids: [], blocks: [] };
    walk(guide.meta, 'meta', found);
    for (const section of guide.sections) walk(section, section.id, found);
    return found;
}

function walk(node: unknown, prefix: string, found: Found): void {
    if (Array.isArray(node)) {
        node.forEach((child, i) => visit(child, `${prefix}.${i}`, found));
        return;
    }
    if (!isObject(node)) return;
    for (const [key, value] of Object.entries(node)) {
        if (isStructure(node, key)) continue;
        if (key === 'blocks') walkBlocks(value as unknown[], prefix, found);
        else visit(value, `${prefix}.${key}`, found);
    }
}

function isObject(node: unknown): node is Record<string, unknown> {
    return !!node && typeof node === 'object';
}

/** Whether one field of one node names structure rather than copy. */
function isStructure(node: Record<string, unknown>, key: string): boolean {
    if (LITERAL.has(key)) return true;
    return key === 'name' && node.kind === 'widget';
}

// Blocks are addressed by position rather than by the `blocks.<i>` the generic
// walk would give them: `tour.b1` reads as "the second block of tour" in a
// translator's file, and the shorter form is what the ids have always been.
function walkBlocks(blocks: unknown[], prefix: string, found: Found): void {
    blocks.forEach((block, i) => {
        const id = `${prefix}.b${i}`;
        found.blocks.push(id);
        walk(block, id, found);
    });
}

function visit(value: unknown, id: string, found: Found): void {
    if (typeof value === 'string') found.ids.push(id);
    else walk(value, id, found);
}

/**
 * The English guide with one locale's copy laid over it. Every id the locale
 * supplies replaces the English string at that position; an id the locale
 * omits keeps English, which is what a half-translated language should render
 * (assertGuideParity refuses to build over a gap, so this is a safety net, not
 * the plan).
 *
 * Setting by path rather than rebuilding the tree is what lets a locale ADD a
 * `labels` entry the English guide has none of: the dashboard mocks under
 * components/guides/screens carry their own English labels, so English
 * supplies no override and French overrides every line.
 */
export function translate(guide: GuideContent, strings: GuideStrings): GuideContent {
    const copy = structuredClone(guide);
    for (const [id, text] of Object.entries(strings)) set(copy, id, text);
    return copy;
}

function set(guide: GuideContent, id: string, text: string): void {
    const [scope, ...trail] = id.split('.');
    const last = trail.pop();
    if (last === undefined) return;
    let node = scopeOf(guide, scope);
    for (const segment of trail) node = step(node, segment);
    if (node) node[last] = text;
}

type Node = Record<string, unknown> | undefined;

function scopeOf(guide: GuideContent, scope: string): Node {
    if (scope === 'meta') return guide.meta as unknown as Record<string, unknown>;
    return guide.sections.find((s) => s.id === scope) as unknown as Node;
}

function step(node: Node, segment: string): Node {
    if (!node) return undefined;
    const block = /^b(\d+)$/.exec(segment);
    if (block) return (node.blocks as Node[] | undefined)?.[Number(block[1])];
    node[segment] ??= {};
    return node[segment] as Node;
}
