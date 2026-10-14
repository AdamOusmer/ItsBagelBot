// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { moduleDef } from '../../kit/lib/catalog/index.ts';
import { blankTimer } from '../../kit/lib/timers.ts';
import { blankDiscordConfig } from '../../kit/lib/discord-config.ts';
import { guildModuleTiles } from '../../kit/lib/discord-overview.ts';

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

async function expectCommonLanding(page, secondaryHref) {
    await expect(page.locator('.flupd time')).toHaveText(/Updated/);
    await expect(page.locator('.flupd time')).toHaveAttribute('datetime', /^\d{4}-\d{2}-\d{2}$/);
    await expect(page.locator('.flcta__btn').nth(1)).toHaveAttribute('href', secondaryHref);
}

const SCHEMA_PAGES = ['/valorant-stats/', '/fr/fortnite-stats/'];

async function landingGraph(page) {
    const raw = await page.locator('head script[type="application/ld+json"]').first().textContent();
    return JSON.parse(raw)['@graph'];
}

test.describe('feature landing pages', () => {
    for (const path of SCHEMA_PAGES) {
        test(`${path} describes itself with WebPage, BreadcrumbList and a command ItemList`, async ({ page }) => {
            await page.goto(path);
            const graph = await landingGraph(page);
            const ofType = (type) => graph.filter((node) => node['@type'] === type);
            const [webPage] = ofType('WebPage');
            const [list] = ofType('ItemList');
            expect(ofType('WebPage')).toHaveLength(1);
            expect(ofType('BreadcrumbList')).toHaveLength(1);
            expect(ofType('ItemList')).toHaveLength(1);
            expect(webPage.dateModified).toBe(await page.locator('.flupd time').getAttribute('datetime'));
            expect(webPage.inLanguage).toBe(path.startsWith('/fr/') ? 'fr' : 'en');
            expect(webPage.url).toBe(await page.locator('head link[rel="canonical"]').getAttribute('href'));
            expect(webPage.about['@id']).toMatch(/\/#software$/);
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
            await expect(page.locator('.fltbl tbody tr').first().locator('td').nth(1)).not.toHaveText('None');
            await expect(page.locator('.fltbl')).toContainText(game.sample);
            await expect(page.locator('.fldemo')).toBeVisible();
            await expect(page.locator('.flprereq')).toBeVisible();
            await expect(page.locator('.flcta__legal')).toContainText(game.legal);
            await expect(page.locator('head link[rel="describedby"][href="https://ai.itsbagelbot.com/features/' + game.ai + '.md"]')).toHaveCount(1);
            await expect(page.locator('.flrel a[href="/command-builder/"]')).toBeVisible();
            await expect(page.locator('.flrel a[href="/valorant-stats/"]')).toBeVisible();
            expect(await page.title()).toMatch(/^.{1,60}$/);
            expect(await page.locator('head meta[name="description"]').getAttribute('content')).toMatch(/^.{1,155}$/);
            await expectCommonLanding(page, '/guides/modules/');
        });

        test(`${game.path} has a French mirror`, async ({ page }) => {
            await page.goto(`/fr${game.path}`);
            expect(await page.title()).toMatch(game.frTitle);
            await expect(page.locator('.flupd time')).toHaveText(/Mis à jour/);
            await expect(page.locator('.fltbl thead')).toContainText('Commande');
            await expect(page.locator('.flcta__btn').nth(1)).toHaveAttribute('href', '/fr/guides/modules/');
        });
    }

    test('song-requests lists every catalog command and links the Spotify guide', async ({ page }) => {
        await page.goto('/song-requests/');
        const cells = (await commandCells(page)).join('\n');
        for (const command of songCommands) expect(cells).toContain(command);
        await expect(page.locator('.fltbl')).toContainText('!songrequest');
        await expect(page.locator('.flprereq')).toContainText('Spotify Premium');
        await expect(page.locator('.flrel a[href="/guides/setup-errors/"]')).toBeVisible();
        await expect(page.locator('.flrel a[href="/import/"]')).toBeVisible();
        await expect(page.locator('.flrel a[href="/pricing/"]')).toBeVisible();
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
        await expect(button).toHaveClass(/is-done/);
        expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('!sr Daft Punk - One More Time');
    });
});

const VS_SLUGS = ['nightbot', 'streamelements', 'fossabot', 'moobot', 'streamlabs-chatbot', 'wizebot'];
const VS_LOCALES = [['en', ''], ['fr', '/fr'], ['de', '/de'], ['es', '/es'], ['pt-br', '/pt-br'], ['ru', '/ru']];
const VS_CASES = VS_SLUGS.flatMap((slug) => VS_LOCALES.map(([lang, prefix]) => ({ slug, lang, prefix })));
const FOOTER_PAGES = [
    '/valorant-stats/', '/fortnite-stats/', '/hypixel-bedwars-stats/', '/mcsr-ranked-stats/', '/clash-royale-stats/', '/codm-stats/',
    '/vs/nightbot/', '/vs/streamelements/', '/vs/fossabot/', '/vs/moobot/', '/vs/streamlabs-chatbot/', '/vs/wizebot/',
];
const FOOTER_CASES = [['', '/'], ['/de', '/de/']].flatMap(([prefix, route]) => FOOTER_PAGES.map((path) => ({ route, href: `${prefix}${path}` })));

for (const { slug, lang, prefix } of VS_CASES) {
    test(`${prefix}/vs/${slug}/ renders sourced rows and matching FAQ schema`, async ({ page }) => {
        await page.goto(`${prefix}/vs/${slug}/`);
        const rows = page.locator('.vs__table tbody tr');
        expect(await rows.count()).toBeGreaterThan(0);
        for (const row of await rows.all()) {
            await expect(row.locator('a[href^="https://"]')).toHaveCount(1);
        }
        await expect(page.locator('html')).toHaveAttribute('lang', lang);
        await expect(page.locator('.vs__reviewed time')).toHaveAttribute('datetime', /^\d{4}-\d{2}-\d{2}$/);
        await expect(page.locator('.vs__actions a').first()).toHaveAttribute('href', `${prefix}/import/`);

        const graph = JSON.parse(await page.locator('head script[type="application/ld+json"]').textContent())['@graph'];
        const faq = graph.find((node) => node['@type'] === 'FAQPage');
        expect(faq.mainEntity).toHaveLength(await page.locator('.vs__faq .bb-disclosure').count());
        expect(graph.find((node) => node['@type'] === 'WebPage').dateModified).toMatch(/^\d{4}-\d{2}-\d{2}$/);
        expect(graph.find((node) => node['@type'] === 'BreadcrumbList').itemListElement).toHaveLength(3);

        const hreflangs = await page.locator('head link[rel="alternate"][hreflang]').evaluateAll((links) => links.map((l) => l.hreflang));
        expect(hreflangs.map((h) => h.toLowerCase()).sort()).toEqual(['de', 'en', 'es', 'fr', 'pt-br', 'ru', 'x-default']);
        await expect(page.locator('head link[rel="describedby"][href="https://ai.itsbagelbot.com/migration.md"]')).toHaveCount(1);
    });
}

for (const { route, href } of FOOTER_CASES) {
    test(`footer links ${href} at ${route}`, async ({ page }) => {
        await page.goto(route);
        await expect(page.locator(`footer a[href="${href}"]`)).toHaveCount(1);
    });
}

test.describe('comparison pages', () => {
    test('footer compare links and language switcher follow the locale', async ({ page }) => {
        await page.goto('/fr/');
        await expect(page.locator('footer a[href="/fr/vs/nightbot/"]')).toBeVisible();
        await page.goto('/de/');
        await expect(page.locator('footer a[href="/de/vs/nightbot/"]')).toBeVisible();
        await page.goto('/vs/streamelements/');
        await expect(page.locator('.bb-lang-switch a[hreflang="de"]').first()).toHaveAttribute('href', '/de/vs/streamelements/');
    });
});

function catalogEntryCount(id) {
    const def = moduleDef(id);
    const commands = def.commands ?? [];
    const extra = def.replies.filter((reply) => reply.command && !commands.some((command) => command.trigger === `!${reply.command}` || command.trigger.startsWith(`!${reply.command} `)));
    return commands.length + new Set(extra.map((reply) => reply.command)).size;
}

const FEATURE_PAGES = [
    { path: '/giveaways/', title: 'Twitch Giveaway Bot', ai: 'engagement', rows: catalogEntryCount('raffle') },
    { path: '/loyalty-points/', title: 'Twitch Loyalty Points Bot', ai: 'loyalty', rows: ['loyalty', 'gamble', 'duel'].map(catalogEntryCount).reduce((sum, count) => sum + count, 0) },
    { path: '/death-counter/', title: 'Twitch Death Counter', ai: 'loyalty', rows: moduleDef('loyalty').commands.filter((command) => command.trigger === '!counter').length },
    { path: '/timers/', title: 'Twitch Timer Bot', ai: 'automation', rows: Object.keys(blankTimer()).filter((key) => key !== 'id').length },
    { path: '/viewer-queue/', title: 'Twitch Viewer Queue Bot', ai: 'engagement', rows: catalogEntryCount('queue') },
    { path: '/discord-bot/', title: 'Twitch Discord Bot', ai: 'discord', rows: guildModuleTiles(blankDiscordConfig()).length },
];

for (const feature of FEATURE_PAGES) {
    test(`${feature.path} renders its kit table, review date and landing schema`, async ({ page }) => {
        await page.goto(feature.path);
        await expect(page.locator('h1')).toBeVisible();
        await expect(page).toHaveTitle(new RegExp(feature.title));
        await expect(page.locator('.fltbl tbody tr')).toHaveCount(feature.rows);
        await expect(page.locator('.flprereq')).toBeVisible();
        await expect(page.locator('.flupd time')).toHaveText('Updated October 14, 2026');
        await expect(page.locator('.flupd time')).toHaveAttribute('datetime', '2026-10-14');
        await expect(page.locator(`head link[rel="describedby"][href="https://ai.itsbagelbot.com/features/${feature.ai}.md"]`)).toHaveCount(1);
        const graph = await landingGraph(page);
        expect(graph.filter((node) => node['@type'] === 'WebPage')).toHaveLength(1);
        expect(graph.filter((node) => node['@type'] === 'BreadcrumbList')).toHaveLength(1);
        expect(graph.find((node) => node['@type'] === 'WebPage').dateModified).toBe('2026-10-14');
        expect(graph.find((node) => node['@type'] === 'ItemList').numberOfItems).toBe(feature.rows);
        expect(await page.title()).toMatch(/^.{1,60}$/);
        expect(await page.locator('head meta[name="description"]').getAttribute('content')).toMatch(/^.{1,155}$/);
        await expect(page.locator('.flrel a')).toHaveCount(3);
    });
}
