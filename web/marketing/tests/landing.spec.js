// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { moduleDef } from '../../kit/lib/catalog/index.ts';
import { blankTimer } from '../../kit/lib/timers.ts';

const valorantCommands = moduleDef('valorant').replies.map((reply) => `!${reply.command}`);
const GAME_PAGES = [
    { path: '/fortnite-stats/', module: 'fortnite', ai: 'fortnite', legal: 'Epic Games', sample: 'Ninja', frTitle: /Fortnite/ },
    { path: '/hypixel-bedwars-stats/', module: 'urchin', ai: 'urchin', legal: 'Hypixel', sample: 'Technoblade', frTitle: /Bedwars/ },
    { path: '/mcsr-ranked-stats/', module: 'mcsr', ai: 'mcsr', legal: 'Mojang Studios', sample: 'Feinberg', frTitle: /MCSR/ },
    { path: '/clash-royale-stats/', module: 'clashroyale', ai: 'clashroyale', legal: 'Supercell', sample: '#P2LQ0GR', frTitle: /Clash Royale/ },
    { path: '/codm-stats/', module: 'codm', ai: 'codm', legal: 'Activision', sample: 'iFerg', frTitle: /CODM/ },
];
const songCommands = moduleDef('songqueue').commands.map((command) => command.trigger);
const IMPORT_SOURCES = ['Nightbot', 'StreamElements', 'Moobot', 'Streamlabs Chatbot', 'Fossabot', 'WizeBot'];

async function commandCells(page) {
    return page.locator('.fltbl tbody tr td:first-child').allInnerTexts();
}

async function expectCommonLanding(page, secondaryHref, updated = /Updated/) {
    await expect(page.locator('.flupd time')).toHaveText(updated);
    await expect(page.locator('.flupd time')).toHaveAttribute('datetime', /^\d{4}-\d{2}-\d{2}$/);
    await expect(page.locator('.flcta__btn').nth(1)).toHaveAttribute('href', secondaryHref);
}

const SCHEMA_PAGES = ['/valorant-stats/', '/fr/fortnite-stats/'];

async function landingGraph(page) {
    const raw = await page.locator('head script[type="application/ld+json"]').first().textContent();
    return JSON.parse(raw)['@graph'];
}

async function expectPageMeta(page) {
    expect(await page.title()).toMatch(/^.{1,60}$/);
    expect(await page.locator('head meta[name="description"]').getAttribute('content')).toMatch(/^.{1,155}$/);
}

async function expectHreflangs(page) {
    const hreflangs = await page.locator('head link[rel="alternate"][hreflang]').evaluateAll((links) => links.map((link) => link.hreflang.toLowerCase()).sort());
    expect(hreflangs).toEqual(['de', 'en', 'es', 'fr', 'pt-br', 'ru', 'x-default']);
}

async function expectComparisonSchema(page, faqSelector) {
    const graph = await landingGraph(page);
    expect(graph.find((node) => node['@type'] === 'FAQPage').mainEntity).toHaveLength(await page.locator(faqSelector).count());
    expect(graph.find((node) => node['@type'] === 'WebPage').dateModified).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(graph.find((node) => node['@type'] === 'BreadcrumbList').itemListElement).toHaveLength(3);
}

async function expectRowSources(rows) {
    for (const row of await rows.all()) {
        await expect(row.locator('a[href^="https://"][target="_blank"]')).toHaveCount(1);
    }
}

function expectLandingNodes(graph) {
    expect(graph.filter((node) => node['@type'] === 'WebPage')).toHaveLength(1);
    expect(graph.filter((node) => node['@type'] === 'BreadcrumbList')).toHaveLength(1);
    expect(graph.filter((node) => node['@type'] === 'ItemList')).toHaveLength(1);
}

async function expectLandingIdentity(page, webPage, path) {
    expect(webPage.inLanguage).toBe(path.startsWith('/fr/') ? 'fr' : 'en');
    expect(webPage.url).toBe(await page.locator('head link[rel="canonical"]').getAttribute('href'));
    expect(webPage.about['@id']).toMatch(/\/#software$/);
}

async function expectGameContent(page, game) {
    await expect(page.locator('.fltbl tbody tr').first().locator('td').nth(1)).not.toHaveText('None');
    await expect(page.locator('.fltbl')).toContainText(game.sample);
    await expect(page.locator('.fldemo')).toBeVisible();
}

async function expectGameLinks(page, game) {
    await expect(page.locator('head link[rel="describedby"][href="https://ai.itsbagelbot.com/features/' + game.ai + '.md"]')).toHaveCount(1);
    await expect(page.locator('.flrel a[href="/command-builder/"]')).toBeVisible();
    await expect(page.locator('.flrel a[href="/valorant-stats/"]')).toBeVisible();
}

async function expectSongRelatedLinks(page) {
    for (const href of ['/guides/setup-errors/', '/import/', '/pricing/']) {
        await expect(page.locator(`.flrel a[href="${href}"]`)).toBeVisible();
    }
}

async function expectFeatureContent(page, feature) {
    await expect(page.locator('h1')).toBeVisible();
    await expect(page).toHaveTitle(new RegExp(feature.title));
    await expect(page.locator('.fltbl tbody tr')).toHaveCount(feature.rows);
}

async function expectFeatureReview(page) {
    await expect(page.locator('.flprereq')).toBeVisible();
    await expect(page.locator('.flupd time')).toHaveText('Updated October 14, 2026');
    await expect(page.locator('.flupd time')).toHaveAttribute('datetime', '2026-10-14');
}

test.describe('feature landing pages', () => {
    for (const path of SCHEMA_PAGES) {
        test(`${path} describes itself with WebPage, BreadcrumbList and a command ItemList`, async ({ page }) => {
            await page.goto(path);
            const graph = await landingGraph(page);
            const webPage = graph.find((node) => node['@type'] === 'WebPage');
            const list = graph.find((node) => node['@type'] === 'ItemList');
            expectLandingNodes(graph);
            expect(webPage.dateModified).toBe(await page.locator('.flupd time').getAttribute('datetime'));
            await expectLandingIdentity(page, webPage, path);
            expect(list.itemListElement).toHaveLength(await page.locator('.fltbl tbody tr').count());
        });
    }

    test('valorant-stats lists every catalog command with an example reply', async ({ page }) => {
        await page.goto('/valorant-stats/');
        const cells = (await commandCells(page)).join('\n');
        for (const command of valorantCommands) expect(cells).toContain(command);
        await expect(page.locator('.fltbl')).toContainText('Frosty#EUW1');
        await expect(page.locator('.fldemo')).toBeVisible();
        await expect(page.locator('.flcta__legal')).toContainText('Riot Games');
        await expectCommonLanding(page, '/guides/modules/');
    });

    for (const game of GAME_PAGES) {
        test(`${game.path} lists every catalog command, aliases and a sample reply`, async ({ page }) => {
            await page.goto(game.path);
            const cells = (await commandCells(page)).join('\n');
            for (const reply of moduleDef(game.module).replies) expect(cells).toContain(`!${reply.command}`);
            await expectGameContent(page, game);
            await expect(page.locator('.flprereq')).toBeVisible();
            await expect(page.locator('.flcta__legal')).toContainText(game.legal);
            await expectGameLinks(page, game);
            await expectPageMeta(page);
            await expectCommonLanding(page, '/guides/modules/');
        });

        test(`${game.path} has a French mirror`, async ({ page }) => {
            await page.goto(`/fr${game.path}`);
            expect(await page.title()).toMatch(game.frTitle);
            await expectCommonLanding(page, '/fr/guides/modules/', /Mis à jour/);
            await expect(page.locator('.fltbl thead')).toContainText('Commande');
        });
    }

    test('song-requests lists every catalog command and links the Spotify guide', async ({ page }) => {
        await page.goto('/song-requests/');
        const cells = (await commandCells(page)).join('\n');
        for (const command of songCommands) expect(cells).toContain(command);
        await expect(page.locator('.fltbl')).toContainText('!songrequest');
        await expect(page.locator('.flprereq')).toContainText('Spotify Premium');
        await expectSongRelatedLinks(page);
        await expectCommonLanding(page, '/guides/spotify-song-requests/');
    });

    test('govee shows a colour name and a hex redemption', async ({ page }) => {
        await page.goto('/govee/');
        const table = page.locator('.fltbl');
        await expect(table).toContainText('set the lights to blue!');
        await expect(table).toContainText('set the lights to #00ccff!');
        await expect(page.locator('.flprereq')).toContainText('colour-capable');
        await expectCommonLanding(page, '/guides/govee-lights/');
    });

    test('import lists the six sources and a converted command', async ({ page }) => {
        await page.goto('/import/');
        const cells = (await commandCells(page)).join('\n');
        for (const source of IMPORT_SOURCES) expect(cells).toContain(source);
        await expect(page.locator('.flconv')).toContainText('{touser}');
        await expect(page.locator('.flcta__btn').first()).toHaveAttribute('href', /\/settings\/import/);
        await expect(page.locator('.flupd time')).toHaveText(/Updated/);
    });

    test('french landing pages translate the new sections', async ({ page }) => {
        await page.goto('/fr/song-requests/');
        await expect(page.locator('.flupd time')).toHaveText(/Mis à jour/);
        await expect(page.locator('.fltbl thead')).toContainText('Commande');
        await expect(page.locator('.flcta__btn').nth(1)).toHaveAttribute('href', '/fr/guides/spotify-song-requests/');
    });

    test('copy buttons mark the command as copied', async ({ page, context }) => {
        await context.grantPermissions(['clipboard-read', 'clipboard-write']);
        await page.goto('/song-requests/');
        const button = page.locator('.fltbl__copy').first();
        await button.click();
        await expect(button).toHaveClass(/is-copied/);
        expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('!sr Daft Punk - One More Time');
    });
});

const VS_MIGRATION_PATHS = {
    nightbot: '/import/',
    streamelements: '/import/',
    fossabot: '/import/',
    moobot: '/import/',
    'streamlabs-chatbot': '/import/',
    wizebot: '/import/',
    spotchbot: '/guides/spotify-song-requests/',
};
const VS_SLUGS = Object.keys(VS_MIGRATION_PATHS);
const VS_LOCALES = [['en', ''], ['fr', '/fr'], ['de', '/de'], ['es', '/es'], ['pt-br', '/pt-br'], ['ru', '/ru']];
const VS_CASES = VS_SLUGS.flatMap((slug) => VS_LOCALES.map(([lang, prefix]) => ({ slug, lang, prefix })));
const FOOTER_PAGES = ['/pricing/', '/guides/', '/command-builder/', '/changelog/', '/contact/'];
const FOOTER_CASES = [['', '/'], ['/de', '/de/']].flatMap(([prefix, route]) => FOOTER_PAGES.map((path) => ({ route, href: `${prefix}${path}` })));

for (const { slug, lang, prefix } of VS_CASES) {
    test(`${prefix}/vs/${slug}/ renders sourced rows and matching FAQ schema`, async ({ page }) => {
        await page.goto(`${prefix}/vs/${slug}/`);
        const rows = page.locator('.vs__table tbody tr');
        expect(await rows.count()).toBeGreaterThan(0);
        await expectRowSources(rows);
        await expect(page.locator('html')).toHaveAttribute('lang', lang);
        await expect(page.locator('.vs__reviewed time')).toHaveAttribute('datetime', /^\d{4}-\d{2}-\d{2}$/);
        await expect(page.locator('.vs__actions a').first()).toHaveAttribute('href', `${prefix}${VS_MIGRATION_PATHS[slug]}`);

        await expectComparisonSchema(page, '.vs__faq .bb-disclosure');
        await expectHreflangs(page);
        await expect(page.locator('head link[rel="describedby"][href="https://ai.itsbagelbot.com/migration.md"]')).toHaveCount(1);
    });
}

for (const [lang, prefix] of VS_LOCALES) {
    test(`${prefix}/vs/song-request-bots/ renders sourced bots and matching FAQ schema`, async ({ page }) => {
        await page.goto(`${prefix}/vs/song-request-bots/`);
        await expect(page.locator('html')).toHaveAttribute('lang', lang);
        const rows = page.locator('.rnd__table tbody tr');
        await expect(rows).toHaveCount(8);
        await expectRowSources(rows);
        await expectComparisonSchema(page, '.rnd__faq .bb-disclosure');
        await expectHreflangs(page);
        await expectPageMeta(page);
    });
}

for (const { route, href } of FOOTER_CASES) {
    test(`footer links ${href} at ${route}`, async ({ page }) => {
        await page.goto(route);
        await expect(page.locator(`footer a[href="${href}"]`)).toHaveCount(1);
    });
}

test.describe('comparison pages', () => {
    test('language switcher follows the locale', async ({ page }) => {
        await page.goto('/vs/streamelements/');
        await expect(page.locator('.bb-lang-switch a[hreflang="de"]').first()).toHaveAttribute('href', '/de/vs/streamelements/');
    });

    test('each page links the other comparisons and the hub', async ({ page }) => {
        await page.goto('/de/vs/nightbot/');
        const links = page.locator('.vs__related a');
        await expect(links).toHaveCount(VS_SLUGS.length);
        await expect(links.last()).toHaveAttribute('href', '/de/vs/');
        await expect(links.last()).toHaveAccessibleName('Alle Vergleiche');
        const sources = page.locator('.vs__sources a');
        await expect(sources.first()).toHaveAccessibleName(/\S/);
        await expect(page.locator('.vs__related a[href="/de/vs/nightbot/"]')).toHaveCount(0);
    });

    for (const [lang, prefix] of VS_LOCALES) {
        test(`${prefix}/vs/ hub links every comparison`, async ({ page }) => {
            await page.goto(`${prefix}/vs/`);
            await expect(page.locator('html')).toHaveAttribute('lang', lang);
            for (const slug of VS_SLUGS) {
                await expect(page.locator(`.vshub__grid a[href="${prefix}/vs/${slug}/"]`)).toHaveCount(1);
            }
            await expectPageMeta(page);
            const graph = JSON.parse(await page.locator('head script[type="application/ld+json"]').textContent())['@graph'];
            expect(graph.find((node) => node['@type'] === 'ItemList').numberOfItems).toBe(VS_SLUGS.length + 1);
        });
    }
});

test('comparison hub links the song request roundup', async ({ page }) => {
    await page.goto('/vs/');
    await expect(page.locator('.vshub__grid a').first()).toHaveAttribute('href', '/vs/song-request-bots/');
});

test('song-requests has eleven FAQ entries and links the roundup', async ({ page }) => {
    await page.goto('/song-requests/');
    const schemas = await page.locator('script[type="application/ld+json"]').allTextContents();
    const nodes = schemas.flatMap((raw) => {
        const schema = JSON.parse(raw);
        return schema['@graph'] ?? [schema];
    });
    expect(nodes.find((node) => node['@type'] === 'FAQPage').mainEntity).toHaveLength(11);
    await expect(page.locator('.flrel a[href="/vs/song-request-bots/"]')).toHaveCount(1);
});

function triggersReply(command, name) {
    return command.trigger === `!${name}` || command.trigger.startsWith(`!${name} `);
}

function catalogEntryCount(id) {
    const def = moduleDef(id);
    const commands = def.commands ?? [];
    const extra = def.replies.filter((reply) => reply.command && !commands.some((command) => triggersReply(command, reply.command)));
    return commands.length + new Set(extra.map((reply) => reply.command)).size;
}

const FEATURE_PAGES = [
    { path: '/giveaways/', title: 'Twitch Giveaway Bot', ai: 'engagement', rows: catalogEntryCount('raffle') },
    { path: '/loyalty-points/', title: 'Twitch Loyalty Points Bot', ai: 'loyalty', rows: ['loyalty', 'gamble', 'duel'].map(catalogEntryCount).reduce((sum, count) => sum + count, 0) },
    { path: '/death-counter/', title: 'Twitch Death Counter', ai: 'engagement', rows: 3 },
    { path: '/timers/', title: 'Twitch Timer Bot', ai: 'engagement', rows: Object.keys(blankTimer()).filter((key) => key !== 'id').length, related: ['/giveaways/', '/viewer-queue/', '/vs/'] },
    { path: '/viewer-queue/', title: 'Twitch Viewer Queue Bot', ai: 'engagement', rows: catalogEntryCount('queue'), related: ['/giveaways/', '/loyalty-points/', '/vs/'] },
];

for (const feature of FEATURE_PAGES) {
    test(`${feature.path} renders its kit table, review date and landing schema`, async ({ page }) => {
        await page.goto(feature.path);
        await expectFeatureContent(page, feature);
        await expectFeatureReview(page);
        await expect(page.locator(`head link[rel="describedby"][href="https://ai.itsbagelbot.com/features/${feature.ai}.md"]`)).toHaveCount(1);
        const graph = await landingGraph(page);
        expectLandingNodes(graph);
        expect(graph.find((node) => node['@type'] === 'WebPage').dateModified).toBe('2026-10-14');
        expect(graph.find((node) => node['@type'] === 'ItemList').numberOfItems).toBe(feature.rows);
        await expectPageMeta(page);
        const related = page.locator('.flrel a');
        await expect(related).toHaveCount(feature.related?.length ?? 4);
        if (feature.related) expect(await related.evaluateAll((links) => links.map((link) => link.getAttribute('href')))).toEqual(feature.related);
        await expect(page.locator('.flrel a').last()).toHaveAttribute('href', '/vs/');
        await expect(page.locator('.flrel a').last()).toHaveAccessibleName('Bot comparisons');
    });
}
