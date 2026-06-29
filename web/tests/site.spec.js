import { test, expect } from '@playwright/test';

test.describe('ItsBagelBot site', () => {
    test('home renders hero + redesigned sections', async ({ page }) => {
        await page.goto('/');

        await expect(page.locator('h1').first()).toContainText('Your Stream');

        const features = page.locator('#features');
        await expect(features.locator('h2')).toContainText('Everything your stream needs');
        await expect(features.locator('[data-card]')).toHaveCount(6);

        const how = page.locator('#how-it-works');
        await expect(how.locator('[data-card]')).toHaveCount(3);
        for (const step of ['Connect', 'Configure', 'Go Live']) {
            await expect(how).toContainText(step);
        }

        await expect(page.locator('.cta-banner')).toContainText('Ready to upgrade your stream?');
    });

    test('pricing renders 3 tiers, faq, and source-available messaging', async ({ page }) => {
        await page.goto('/pricing');

        await expect(page.locator('.page-hero__title')).toContainText('Simple, honest pricing.');

        const tiers = page.locator('.pricing-cards [data-card]');
        await expect(tiers).toHaveCount(3);
        await expect(tiers.nth(0)).toContainText('Free');
        await expect(tiers.nth(1)).toContainText('Premium');
        await expect(tiers.nth(2)).toContainText('Enterprise');

        // Premium is the highlighted/recommended tier
        await expect(tiers.nth(1)).toContainText('Recommended');

        await expect(page.locator('details')).toHaveCount(5);

        // Source available, never positioned as "open source"
        // (FAQ answer lives in a collapsed <details>, so assert presence, not visibility)
        await expect(page.getByText('source available, not open source')).toHaveCount(1);
    });

    test('contact renders 4 channels with copy affordance', async ({ page }) => {
        await page.goto('/contact');

        await expect(page.locator('.page-hero__title')).toContainText('Get in touch.');
        await expect(page.locator('[data-card]')).toHaveCount(4);
        await expect(page.locator('[data-card]').filter({ hasText: 'Discord' })).toHaveCount(1);
        await expect(page.locator('[data-card]').filter({ hasText: 'General Support' })).toHaveCount(1);

        // Email channels expose a click-to-copy control
        await expect(page.locator('[data-copy]')).toHaveCount(2);
    });

    test('legal pages render with copy intact', async ({ page }) => {
        await page.goto('/privacy');
        await expect(page.locator('.page-hero__title')).toContainText('Privacy Policy');
        await expect(page.locator('body')).toContainText('Data We Collect');

        await page.goto('/terms');
        await expect(page.locator('.page-hero__title')).toContainText('Terms of Service');
        await expect(page.locator('body')).toContainText('Acceptable Use');
        await expect(page.locator('body')).toContainText('Source Available License');
    });

    test('active nav route is marked', async ({ page }) => {
        await page.goto('/pricing');
        await expect(page.locator('nav .is-active')).toContainText('Pricing');
    });
});

test.describe('reduced motion', () => {
    test.use({ reducedMotion: 'reduce' });

    test('reveal content is visible without motion', async ({ page }) => {
        await page.goto('/pricing');
        const tier = page.locator('.pricing-cards [data-card]').first();
        await expect(tier).toBeVisible();
        await expect(tier).toHaveCSS('opacity', '1');
    });
});
