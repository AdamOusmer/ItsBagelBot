// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { moduleDef } from '../../kit/lib/catalog/index.ts';

const valorantCommands = moduleDef('valorant').replies.map((reply) => `!${reply.command}`);
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
