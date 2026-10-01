// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const REPO_ROOT = fileURLToPath(new URL('../../..', import.meta.url));
const PROBES = fileURLToPath(new URL('./layout-probes.js', import.meta.url));

const MANIFEST = JSON.parse(readFileSync(`${REPO_ROOT}locales/manifest.json`, 'utf8'));
const LANG_TS = readFileSync(`${REPO_ROOT}web/marketing/src/i18n/lang.ts`, 'utf8');
const DEFAULT_LANG_MATCH = LANG_TS.match(/defaultLang:\s*Lang\s*=\s*'([^']+)'/);

if (!DEFAULT_LANG_MATCH) {
    throw new Error('locale-layout.spec.js: could not read defaultLang from web/marketing/src/i18n/lang.ts');
}
if (!Array.isArray(MANIFEST) || MANIFEST.length === 0) {
    throw new Error('locale-layout.spec.js: locales/manifest.json did not parse as a non-empty array');
}

const DEFAULT_LANG = DEFAULT_LANG_MATCH[1];

if (!MANIFEST.includes(DEFAULT_LANG)) {
    throw new Error(`locale-layout.spec.js: locales/manifest.json is missing the default locale ${DEFAULT_LANG}`);
}

const LOCALES = [...MANIFEST].sort();
const HERO_MIN_DESKTOP_WIDTH = 901;

function localizedUrl(path, locale) {
    return locale === DEFAULT_LANG ? path : `/${locale}${path}`;
}

const PAGES = [
    { name: 'home', path: '/', hero: true },
    { name: 'pricing', path: '/pricing' },
    { name: 'contact', path: '/contact' },
    { name: 'import', path: '/import' },
    { name: 'guides index', path: '/guides' },
    { name: 'guide: commands', path: '/guides/commands' },
    { name: 'command builder', path: '/command-builder' },
    { name: 'changelog index', path: '/changelog' },
];

// Lives outside [...lang]: always English, never locale-prefixed.
const INVARIANT_PAGES = [
    { name: 'vs nightbot', path: '/vs/nightbot' },
];

function pagesFor(locale) {
    return locale === DEFAULT_LANG ? [...PAGES, ...INVARIANT_PAGES] : PAGES;
}

const VIEWPORTS = [
    { label: '1600x900', width: 1600, height: 900 },
    { label: '1440x900', width: 1440, height: 900 },
    { label: '1280x720', width: 1280, height: 720 },
    { label: '1180x800', width: 1180, height: 800 },
    { label: '1100x800', width: 1100, height: 800 },
    { label: '1024x768', width: 1024, height: 768 },
    { label: '900x900', width: 900, height: 900 },
    { label: '390x844', width: 390, height: 844 },
    { label: '360x640', width: 360, height: 640 },
];

const EXPANDED_OR_COLLAPSED = /^(expanded|collapsed)$/;

const probe = (page, name, arg) =>
    page.evaluate(([fn, value]) => window.layoutProbes[fn](value), [name, arg]);

async function openPage(context, path) {
    const page = await context.newPage();
    await page.goto(path);
    await settle(page);
    return page;
}

async function settle(page) {
    await page.evaluate(() => document.fonts.ready.then(() => undefined));
    await probe(page, 'twoFrames');
    await page.waitForFunction(() => window.layoutProbes.navIdle(), null, { timeout: 3000 });
}

async function expectNoOverflow(page, where) {
    expect(await probe(page, 'hasHorizontalOverflow'), `${where}: document has horizontal overflow`).toBe(false);
}

async function expectNav(page, where) {
    const nav = await probe(page, 'checkNav');
    expect(nav.state, `${where}: nav is in a half/broken state: ${JSON.stringify(nav)}`).toMatch(EXPANDED_OR_COLLAPSED);
    return nav;
}

async function expectMenuOpens(page, where) {
    await page.click('[data-menu-toggle]');
    const menu = await probe(page, 'menuState');
    expect(menu, `${where}: the hamburger must open the menu with a language switch`).toEqual({
        expanded: true,
        open: true,
        hasLanguageSwitch: true,
    });
    await page.keyboard.press('Escape');
}

async function expectHero(page, where) {
    const desktop = await probe(page, 'checkHeroDesktop', HERO_MIN_DESKTOP_WIDTH);
    expect(desktop.ok, `${where}: hero aside/glyph containment broken: ${JSON.stringify(desktop)}`).toBe(true);
    const clipped = await probe(page, 'checkHeroGlyphClip');
    expect(clipped, `${where}: hero glyphs clipped horizontally: ${JSON.stringify(clipped)}`).toEqual([]);
}

async function expectNoClippedText(page, where) {
    const clipped = await probe(page, 'checkClippedText');
    expect(clipped, `${where}: visible text is clipped: ${JSON.stringify(clipped)}`).toEqual([]);
}

async function expectLayout(page, where, pageSpec) {
    await expectNoOverflow(page, where);
    const nav = await expectNav(page, where);
    if (pageSpec.hero) await expectHero(page, where);
    await expectNoClippedText(page, where);
    return nav;
}

async function expectStressedLayout(page, where, pageSpec) {
    await probe(page, 'pseudoLocaleStress');
    await probe(page, 'twoFrames');
    await expectLayout(page, `${where} [pseudo-locale stress]`, pageSpec);
}

function expectWithin(actual, expected, message) {
    expect(Math.abs(actual - expected), message).toBeLessThanOrEqual(1);
}

function expectSameNavGroup(group, where) {
    const [baseline, ...rest] = group;
    const baseItems = new Map((baseline?.nav.items ?? []).map((i) => [i.id, i.centerY]));
    for (const r of rest) {
        expectWithin(r.nav.innerHeight, baseline.nav.innerHeight,
            `${where}: ${r.locale} pill height ${r.nav.innerHeight}px vs ${baseline.locale} ${baseline.nav.innerHeight}px`);
        const comparable = (r.nav.items ?? []).filter((item) => baseItems.has(item.id));
        for (const item of comparable) {
            expectWithin(item.centerY, baseItems.get(item.id),
                `${where}: ${r.locale} ${item.label} centerY ${item.centerY}px vs ${baseline.locale}`);
        }
    }
}

function expectSameHero(results, where) {
    const [baseline, ...rest] = results;
    for (const r of rest) {
        expect(r.hero?.cols, `${where}: ${r.locale} grid columns differ from ${baseline.locale}`).toHaveLength(baseline.hero.cols.length);
        r.hero.cols.forEach((col, i) =>
            expectWithin(col, baseline.hero.cols[i], `${where}: ${r.locale} column ${i} is ${col}px vs ${baseline.hero.cols[i]}px`));
        expectWithin(r.hero.fontSize, baseline.hero.fontSize,
            `${where}: ${r.locale} h1 font-size is ${r.hero.fontSize}px vs ${baseline.hero.fontSize}px`);
    }
}

async function collectGeometry(context, desktop) {
    const results = [];
    for (const locale of LOCALES) {
        const page = await openPage(context, localizedUrl('/', locale));
        const nav = await probe(page, 'checkNav');
        const hero = desktop ? await probe(page, 'checkHeroGeometry') : null;
        results.push({ locale, nav, hero });
        await page.close();
    }
    return results;
}

test.beforeEach(async ({ context }) => {
    await context.addInitScript({ path: PROBES });
});

for (const locale of LOCALES) {
    test.describe.parallel(`locale: ${locale}`, () => {
        for (const viewport of VIEWPORTS) {
            for (const pageSpec of pagesFor(locale)) {
                test(`${pageSpec.name} @ ${viewport.label}`, async ({ context }) => {
                    const page = await context.newPage();
                    await page.emulateMedia({ reducedMotion: 'reduce' });
                    await page.setViewportSize({ width: viewport.width, height: viewport.height });
                    await page.goto(localizedUrl(pageSpec.path, locale));
                    await settle(page);

                    const where = `${locale} ${viewport.label} ${pageSpec.name}`;
                    const nav = await expectLayout(page, where, pageSpec);
                    if (nav.state === 'collapsed') await expectMenuOpens(page, where);
                    if (locale === DEFAULT_LANG) await expectStressedLayout(page, where, pageSpec);
                });
            }
        }
    });
}

test.describe('layout geometry does not depend on locale', () => {
    for (const viewport of VIEWPORTS) {
        test(`hero + nav geometry match across locales @ ${viewport.label}`, async ({ browser }) => {
            const context = await browser.newContext({
                viewport: { width: viewport.width, height: viewport.height },
                reducedMotion: 'reduce',
            });
            await context.addInitScript({ path: PROBES });
            try {
                const desktop = viewport.width >= HERO_MIN_DESKTOP_WIDTH;
                const results = await collectGeometry(context, desktop);
                const where = `geometry @ ${viewport.label}`;
                for (const r of results) {
                    expect(r.nav.state, `${where}: ${r.locale} nav is in a half/broken state: ${JSON.stringify(r.nav)}`).toMatch(EXPANDED_OR_COLLAPSED);
                }
                for (const state of ['expanded', 'collapsed']) {
                    expectSameNavGroup(results.filter((r) => r.nav.state === state), `${where} (${state} nav)`);
                }
                if (desktop) expectSameHero(results, where);
            } finally {
                await context.close();
            }
        });
    }
});

test.describe('hero intro motion settles into the same geometry', () => {
    for (const locale of LOCALES) {
        test(`glyphs land inside the header once the intro finishes: ${locale}`, async ({ context }) => {
            const page = await context.newPage();
            await page.setViewportSize({ width: 1440, height: 900 });
            await page.goto(localizedUrl('/', locale));
            await page.waitForFunction(() => window.layoutProbes.glyphsSettled(), null, { timeout: 8000 });
            await expectHero(page, `${locale} after the intro`);
        });
    }
});
