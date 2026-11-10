// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';

const headers = readFileSync(new URL('../dist/_headers', import.meta.url), 'utf8');
const policy = headers.match(/^\s+Content-Security-Policy: (.+)$/m)[1];

function collectBrowserErrors(page) {
    const violations = [];
    const errors = [];
    page.on('console', (message) => {
        if (message.type() === 'error' && /Content Security Policy/.test(message.text())) {
            violations.push(message.text());
        }
    });
    page.on('pageerror', (error) => errors.push(error.message));
    return { violations, errors };
}

async function mockCloudflareScripts(page) {
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
}

function cloudflareInjection(path) {
    return `<script>
        /* Per-response Cloudflare bootstrap: ${path} */
        (function () {
            document.documentElement.dataset.bootstrap = location.pathname;
            window.__bootstrapRuns = (window.__bootstrapRuns ?? 0) + 1;
            const challenge = document.createElement('script');
            challenge.src = '/cdn-cgi/challenge-platform/scripts/jsd/api.js';
            document.head.append(challenge);
        })();
    </script>
    <script src="https://static.cloudflareinsights.com/beacon.min.js"></script>
    <script src="https://ajax.cloudflare.com/cdn-cgi/scripts/test/rocket-loader.min.js"></script>`;
}

async function serveWithProductionPolicy(route) {
    const path = new URL(route.request().url()).pathname;
    if (path.startsWith('/cdn-cgi/')) {
        await route.fallback();
        return;
    }
    const response = await route.fetch();
    if (!response.headers()['content-type']?.includes('text/html')) {
        await route.fulfill({ response });
        return;
    }
    await route.fulfill({
        response,
        headers: { ...response.headers(), 'content-security-policy': policy },
        body: deferUnmarkedModules(await response.text()).replace('</head>', `${cloudflareInjection(path)}</head>`),
    });
}

function deferUnmarkedModules(html) {
    // Rocket Loader retypes every script that lacks data-cfasync="false" ahead of its src.
    return html.replaceAll('<script type="module"', '<script type="edge-deferred-module"');
}

async function serveAsProduction(page) {
    await mockCloudflareScripts(page);
    // Astro preview does not apply Pages' _headers. Enforce both real policies.
    await page.route('http://localhost:4399/**', serveWithProductionPolicy);
}

test('Cloudflare scripts and site interactions work across client navigation', async ({ page }) => {
    const { violations, errors } = collectBrowserErrors(page);
    await serveAsProduction(page);

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
        expect(await page.evaluate(() => window.__cspNavigationMarker)).toBe(true);
    }
    expect(await page.evaluate(() => window.__bootstrapRuns)).toBe(1);
    expect(violations).toEqual([]);
    expect(errors).toEqual([]);
});

test('each page runs its own scripts after repeated client navigation', async ({ page }) => {
    const { errors } = collectBrowserErrors(page);
    await serveAsProduction(page);
    await page.route('https://status.itsbagelbot.com/**', (route) => route.fulfill({
        json: { data: { attributes: { aggregate_state: 'operational' } } },
        headers: { 'access-control-allow-origin': '*' },
    }));

    await page.goto('/');
    await page.evaluate(() => { window.__cspNavigationMarker = true; });
    for (const [selector, path] of [
        ['.bb-footer a[href="/privacy/"]', '/privacy/'],
        ['.bb-nav__brand', '/'],
        ['.bb-nav__links a[href="/contact/"]', '/contact/'],
    ]) {
        await page.locator(selector).first().click();
        await expect(page).toHaveURL(new RegExp(`${path}$`));
    }

    await expect(page.locator('.contact-status')).toHaveClass(/is-operational/);
    expect(await page.evaluate(() => window.__cspNavigationMarker)).toBe(true);
    expect(await page.locator('script[type="edge-deferred-module"]').count()).toBe(0);
    expect(errors).toEqual([]);
});

test('hovering a link warms the page with a plain fetch', async ({ page }) => {
    await page.goto('/');
    const warmed = page.waitForRequest((request) => new URL(request.url()).pathname === '/pricing/');
    await page.locator('.bb-nav__links a[href="/pricing/"]').hover();

    const request = await warmed;
    expect(request.resourceType()).toBe('fetch');
    expect(request.headers()['sec-purpose']).toBeUndefined();
    expect(await page.locator('link[rel="prefetch"]').count()).toBe(0);
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
