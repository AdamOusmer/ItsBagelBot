import { test, expect } from '@playwright/test';

test.describe('ItsBagelBot site', () => {
    test('home renders hero + Act II sections', async ({ page }) => {
        await page.goto('/');

        await expect(page.locator('h1').first()).toContainText('Your Stream');

        // Encryption act is untouched
        await expect(page.locator('#enc-section')).toHaveCount(1);

        // Playground: chat window, command chips, spam button, live feed seed
        const play = page.locator('#playground');
        await expect(play.locator('h2')).toContainText('Go on, poke it.');
        await expect(play.locator('[data-play-cmd]')).toHaveCount(4);
        await expect(play.locator('[data-play-spam]')).toHaveCount(1);
        await expect(play.locator('[data-play-feed] .pmsg')).not.toHaveCount(0);

        // Playground responds to a command
        await play.locator('[data-play-input]').fill('!bagel');
        await play.locator('.play__send').click();
        await expect(play.locator('.pmsg--you').last()).toContainText('!bagel');
        await expect(play.locator('.pmsg--bot').last()).toContainText('fresh from the oven', { timeout: 5000 });

        // Quiet work bento
        const quiet = page.locator('#quiet-work');
        await expect(quiet.locator('[data-card]')).toHaveCount(6);
        await expect(quiet).toContainText('It catches the noise before you do.');

        // Steps
        const how = page.locator('#how');
        await expect(how.locator('.step')).toHaveCount(3);
        for (const step of ['Connect your channel.', 'Make it yours.', 'Go live, and breathe.']) {
            await expect(how).toContainText(step);
        }

        // Letter + finale
        await expect(page.locator('#letter')).toContainText('no trackers, no data sold');
        await expect(page.locator('[data-letter-stamp]')).toHaveCount(1);
        await expect(page.locator('.finale')).toContainText('Stream');
    });

    test('pricing renders free-first tiers, oath, and faq', async ({ page }) => {
        await page.goto('/pricing');

        await expect(page.locator('.phero__title')).toContainText('Free is the whole product.');

        const tiers = page.locator('.tiers [data-card]');
        await expect(tiers).toHaveCount(3);
        await expect(tiers.nth(0)).toContainText('Free');
        await expect(tiers.nth(1)).toContainText('Premium');
        await expect(tiers.nth(2)).toContainText('Enterprise');

        // Premium framed as the tip jar, not the "recommended" upsell
        await expect(tiers.nth(1)).toContainText('The tip jar, with perks');

        await expect(page.locator('.tiers__oath')).toContainText('no feature gates');

        await expect(page.locator('details')).toHaveCount(5);

        // Source available, never positioned as "open source"
        // (FAQ answer lives in a collapsed <details>, so assert presence, not visibility)
        await expect(page.getByText('source available, not open source')).toHaveCount(1);
    });

    test('contact renders 4 switchboard lines with copy affordance', async ({ page }) => {
        await page.goto('/contact');

        await expect(page.locator('.phero__title')).toContainText('Talk to a human.');
        await expect(page.locator('.board .line')).toHaveCount(4);
        await expect(page.locator('.line').filter({ hasText: 'Discord' })).toHaveCount(1);
        await expect(page.locator('.line').filter({ hasText: 'Support' })).toHaveCount(1);

        // Email lines expose a click-to-copy control
        await expect(page.locator('[data-copy]')).toHaveCount(2);
    });

    test('legal pages render with toc, plain words, and copy intact', async ({ page }) => {
        await page.goto('/privacy');
        await expect(page.locator('.phero__title')).toContainText('Privacy Policy');
        await expect(page.locator('body')).toContainText('Data We Collect');
        await expect(page.locator('[data-legal-link]')).toHaveCount(8);
        await expect(page.locator('.lshell__plain').first()).toContainText('plain words');

        await page.goto('/terms');
        await expect(page.locator('.phero__title')).toContainText('Terms of Service');
        await expect(page.locator('body')).toContainText('Acceptable Use');
        await expect(page.locator('body')).toContainText('Source Available License');
        await expect(page.locator('[data-legal-link]')).toHaveCount(10);
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
        const tier = page.locator('.tiers [data-card]').first();
        await expect(tier).toBeVisible();
        await expect(tier).toHaveCSS('opacity', '1');
    });
});
