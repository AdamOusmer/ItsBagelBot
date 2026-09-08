// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The guide skeleton: a guide's structure, once, with a Key wherever a
 * translatable string used to sit.
 *
 * Why: a guide's structure is not copy. Section ids, block kinds, which screen
 * a mock shows, how many columns a card grid has, who says which chat line --
 * none of it changes between languages, and all of it was written out again in
 * full for every locale. Five guides at ~230 structural lines each, times every
 * language we ever add. The two locales had stayed in step only because
 * parity.ts refused to build when they did not, which is a guard against a
 * problem that should not exist.
 *
 * So: `lib/guides/skeletons/<slug>.ts` holds the structure with keys, and
 * `content/guides/<slug>.<lang>.ts` is a flat map from key to text. Adding a
 * language is now one flat file of strings with no structure to get wrong, and
 * a translator never sees a block union.
 *
 * The alternative was to keep the shape in every locale and lean harder on the
 * parity guard. That is what this replaces: it caught drift, but only after
 * someone had already written the structure a second time by hand.
 */
import type { GuideContent } from './types';

/**
 * A stand-in for a translatable string: `k('intro.b0.html')` names an entry in
 * the locale's string map.
 *
 * A class rather than a sigil-prefixed string ('@intro.b0.html') because the
 * skeleton also carries real strings that must survive verbatim -- a section
 * id, a dashboard path, a widget name -- and a marker that lives in the type
 * system cannot be confused with copy that happens to start with the sigil.
 */
export class Key {
    constructor(readonly id: string) {}
}

export function k(id: string): Key {
    return new Key(id);
}

/** T with every string position free to hold either a literal or a Key. */
type Skel<T> = T extends string
    ? T | Key
    : T extends readonly (infer E)[]
      ? Skel<E>[]
      : T extends object
        ? { [P in keyof T]: Skel<T[P]> }
        : T;

/** A guide's structure: GuideContent with Keys where the copy goes. */
export type GuideSkeleton = Skel<GuideContent>;

/** One locale's copy for one guide: every key the skeleton names. */
export type GuideStrings = Record<string, string>;

/**
 * Fill a skeleton with one locale's strings. A key the locale is missing falls
 * back to English, so a half-translated language renders rather than showing a
 * reader a raw key; assertGuideParity() is what fails the build over drift, in
 * the same place it used to fail over a dropped section.
 *
 * A key that neither map has drops its property rather than filling it with a
 * placeholder, and that case is normal, not an error: the dashboard mocks under
 * components/guides/screens carry their own English labels, so an English guide
 * supplies no `labels` at all and a French one overrides every line. The
 * skeleton names the union, and each locale answers the part that is its own.
 */
export function hydrate(
    skeleton: GuideSkeleton,
    strings: GuideStrings,
    fallback: GuideStrings,
): GuideContent {
    return fill(skeleton, strings, fallback) as GuideContent;
}

function fill(node: unknown, strings: GuideStrings, fallback: GuideStrings): unknown {
    if (node instanceof Key) return strings[node.id] ?? fallback[node.id];
    if (Array.isArray(node)) return node.map((child) => fill(child, strings, fallback));
    if (node && typeof node === 'object') return fillObject(node, strings, fallback);
    return node;
}

function fillObject(node: object, strings: GuideStrings, fallback: GuideStrings): object {
    const out: Record<string, unknown> = {};
    for (const [name, child] of Object.entries(node)) {
        const value = fill(child, strings, fallback);
        if (value !== undefined) out[name] = value;
    }
    return out;
}

/** Every key a skeleton names, in traversal order. Drives the parity check. */
export function skeletonKeys(skeleton: GuideSkeleton): string[] {
    const found: string[] = [];
    collect(skeleton, found);
    return found;
}

function collect(node: unknown, found: string[]): void {
    if (node instanceof Key) {
        found.push(node.id);
        return;
    }
    for (const child of children(node)) collect(child, found);
}

// Arrays and plain objects are the only containers a skeleton nests; a
// Key is a leaf and anything else (string, number, null) has no children.
function children(node: unknown): unknown[] {
    if (Array.isArray(node)) return node;
    if (node && typeof node === 'object') return Object.values(node);
    return [];
}
