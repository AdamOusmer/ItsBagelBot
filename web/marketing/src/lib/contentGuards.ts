// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const EM_DASH = '—';

const sources = import.meta.glob<string>(
    ['../content/**/*.{md,ts,json}', '../i18n/locales/*.json'],
    { eager: true, query: '?raw', import: 'default' },
);

const legalFiles = import.meta.glob('../content/legal/*/*/*.{md,json}');

function fail(reason: string): never {
    throw new Error(`content guard: ${reason}`);
}

function assertNoEmDash(): void {
    for (const path in sources) {
        if (sources[path].includes(EM_DASH)) fail(`${path} contains an em dash`);
    }
}

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
