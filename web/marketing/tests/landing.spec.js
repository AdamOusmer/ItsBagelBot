// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { moduleDef } from '../../kit/lib/catalog/index.ts';

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

test.describe('feature landing pages', () => {
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

test.describe('comparison pages', () => {
    for (const slug of ['nightbot', 'streamelements', 'fossabot', 'moobot', 'streamlabs-chatbot', 'wizebot']) {
        for (const [lang, prefix] of [['en', ''], ['fr', '/fr']]) {
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
                expect(hreflangs.sort()).toEqual(['en', 'fr', 'x-default']);
                await expect(page.locator('head link[rel="describedby"][href="https://ai.itsbagelbot.com/migration.md"]')).toHaveCount(1);
            });
        }

        test(`/de/vs/${slug}/ does not exist`, async ({ request }) => {
            expect((await request.get(`/de/vs/${slug}/`)).status()).toBe(404);
        });
    }

    test('footer compare links follow the locale only where a translation exists', async ({ page }) => {
        await page.goto('/fr/');
        await expect(page.locator('footer a[href="/fr/vs/nightbot/"]')).toBeVisible();
        await page.goto('/de/');
        await expect(page.locator('footer a[href="/vs/nightbot/"]')).toBeVisible();
        await page.goto('/vs/streamelements/');
        await expect(page.locator('.bb-lang-switch a[hreflang="de"]').first()).toHaveAttribute('href', '/vs/streamelements/');
    });
});
