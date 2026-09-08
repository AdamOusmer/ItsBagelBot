// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Build-time guards on everything under src/content and src/i18n/locales.
 * Imported for its side effect by Layout.astro, which every page renders
 * through, so `astro build` and `astro dev` fail loudly instead of shipping.
 *
 * Two checks, both about copy the type system cannot see:
 *
 *  - a legal document whose locales do not carry the same section files.
 *    LegalPage.astro sorts NN-<anchor>.md by filename and derives each
 *    section's anchor from it, so a dropped or renamed French file silently
 *    renumbers the TOC and breaks every deep link into that document. It used
 *    to only warn when a locale had NO sections at all, which is the one case
 *    that is obvious anyway.
 *  - an em dash anywhere in the site's copy. The house style is commas,
 *    parentheses and colons; an em dash reads as machine-written. This lives
 *    here rather than in the guides parity check it grew up in, because the
 *    rule was never about guides: legal markdown and the locale catalogs are
 *    the same prose and had no check at all.
 */

// Written as an escape, not the character: this file is inside the tree it
// scans and the literal would be its own first false positive.
const EM_DASH = '—';

// Raw text of every content file, bundled at build time. Server-side only:
// Layout.astro's frontmatter is the only importer, so none of this reaches a
// client bundle.
const sources = import.meta.glob<string>(
    ['../content/**/*.{md,ts,json}', '../i18n/locales/*.json'],
    { eager: true, query: '?raw', import: 'default' },
);

// Paths only (lazy glob, nothing loaded): the legal section files, keyed
// '../content/legal/<doc>/<locale>/<file>'.
const legalFiles = import.meta.glob('../content/legal/*/*/*.{md,json}');

function fail(reason: string): never {
    throw new Error(`content guard: ${reason}`);
}

function assertNoEmDash(): void {
    for (const path in sources) {
        if (sources[path].includes(EM_DASH)) fail(`${path} contains an em dash`);
    }
}

/** '<doc>/<locale>' -> the section filenames it holds, sorted as the page sorts them. */
function legalSections(): Map<string, string[]> {
    const byDir = new Map<string, string[]>();
    for (const path in legalFiles) {
        const [doc, locale, file] = path.split('/').slice(-3);
        const key = `${doc}/${locale}`;
        byDir.set(key, [...(byDir.get(key) ?? []), file]);
    }
    for (const files of byDir.values()) files.sort();
    return byDir;
}

function assertLegalParity(): void {
    const byDir = legalSections();
    const english = new Map(
        [...byDir].filter(([key]) => key.endsWith('/en')).map(([key, files]) => [key.split('/')[0], files]),
    );
    for (const [key, files] of byDir) {
        const [doc, locale] = key.split('/');
        const reference = english.get(doc) ?? fail(`"${doc}" has no en sections`);
        if (locale === 'en' || files.join() === reference.join()) continue;
        fail(`"${doc}" ${locale} sections are ${files.join(', ')}, en has ${reference.join(', ')}`);
    }
}

assertNoEmDash();
assertLegalParity();
