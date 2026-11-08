// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';

const headers = readFileSync(new URL('../dist/_headers', import.meta.url), 'utf8');
const policy = headers.match(/^\s+Content-Security-Policy: (.+)$/m)[1];

test('Cloudflare scripts and site interactions work across client navigation', async ({ page }) => {
    const violations = [];
    const errors = [];
    page.on('console', (message) => {
        if (message.type() === 'error' && /Content Security Policy/.test(message.text())) {
            violations.push(message.text());
        }
    });
    page.on('pageerror', (error) => errors.push(error.message));

    // Exercise the real browser CSP with each Cloudflare script origin, without
    // sending test analytics or executing a real bot challenge in CI.
    for (const [url, flag] of [
        ['https://static.cloudflareinsights.com/beacon.min.js', 'analytics'],
        ['https://ajax.cloudflare.com/cdn-cgi/scripts/test/rocket-loader.min.js', 'rocket'],
        ['**/cdn-cgi/challenge-platform/scripts/jsd/api.js', 'challenge'],
    ]) {
        await page.route(url, (route) => route.fulfill({
            contentType: 'application/javascript',
            body: `document.documentElement.dataset.${flag} = 'loaded';`,
        }));
    }

    // Astro preview does not apply Pages' _headers. Add the deployed header in
    // addition to the layout's real meta CSP, and emulate Cloudflare injection.
    await page.route('http://localhost:4399/**', async (route) => {
        if (new URL(route.request().url()).pathname.startsWith('/cdn-cgi/')) {
            await route.fallback();
            return;
        }
        const response = await route.fetch();
        if (!response.headers()['content-type']?.includes('text/html')) {
            await route.fulfill({ response });
            return;
        }
        const path = new URL(route.request().url()).pathname;
        const injection = `<script>
            /* Per-response Cloudflare bootstrap: ${path} */
            (function () {
                document.documentElement.dataset.bootstrap = location.pathname;
                const challenge = document.createElement('script');
                challenge.src = '/cdn-cgi/challenge-platform/scripts/jsd/api.js';
                document.head.append(challenge);
            })();
        </script>
        <script src="https://static.cloudflareinsights.com/beacon.min.js"></script>
        <script src="https://ajax.cloudflare.com/cdn-cgi/scripts/test/rocket-loader.min.js"></script>`;
        await route.fulfill({
            response,
            headers: { ...response.headers(), 'content-security-policy': policy },
            body: (await response.text()).replace('</head>', `${injection}</head>`),
        });
    });

    await page.goto('/');
    for (const flag of ['analytics', 'rocket', 'challenge']) {
        await expect(page.locator('html')).toHaveAttribute(`data-${flag}`, 'loaded');
    }
    await expect(page.locator('html')).toHaveAttribute('data-bootstrap', '/');
    await page.locator('.play-input').fill('!bagel');
    await page.locator('.play__send').click();
    await expect(page.locator('.pmsg--bot').last()).toContainText('fresh from the oven');

    // A document reload would discard this marker, so this verifies actual swaps.
    await page.evaluate(() => { window.__cspNavigationMarker = true; });
    for (const path of ['/pricing/', '/guides/']) {
        await page.locator(`.bb-nav__links a[href="${path}"]`).click();
        await expect(page).toHaveURL(new RegExp(`${path}$`));
        await expect(page.locator('html')).toHaveAttribute('data-bootstrap', path);
        expect(await page.evaluate(() => window.__cspNavigationMarker)).toBe(true);
    }
    expect(violations).toEqual([]);
    expect(errors).toEqual([]);
});

test('production CSP still blocks inline event handlers and eval', async ({ page }) => {
    await page.route('**/csp-check', (route) => route.fulfill({
        contentType: 'text/html',
        headers: { 'content-security-policy': policy },
        body: `<button onclick="document.body.dataset.handler = 'ran'">Run handler</button>
            <script>
                try { eval('1 + 1'); }
                catch { document.documentElement.dataset.eval = 'blocked'; }
            </script>`,
    }));
    await page.goto('/csp-check');
    await expect(page.locator('html')).toHaveAttribute('data-eval', 'blocked');
    await page.getByRole('button', { name: 'Run handler' }).click();
    await expect(page.locator('body')).not.toHaveAttribute('data-handler', 'ran');
});
