import { test, expect } from '@playwright/test';

test.describe('ItsBagelBot site', () => {
    async function jumpDown(page) {
        await page.evaluate(() => {
            const maxScroll = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
            const target = Math.min(Math.max(window.innerHeight * 1.4, 700), maxScroll);
            window.lenis?.scrollTo?.(target, { immediate: true, force: true });
            window.scrollTo({ top: target, behavior: 'instant' });
        });
        await page.waitForFunction(() => window.scrollY > 500);
    }

    async function expectPageTop(page) {
        await page.waitForFunction(() => {
            const lenis = window.lenis;
            const lenisScroll = typeof lenis?.scroll === 'number' ? lenis.scroll : 0;
            const lenisTarget = typeof lenis?.targetScroll === 'number' ? lenis.targetScroll : 0;
            const savedScroll = typeof history.state?.scrollY === 'number' ? history.state.scrollY : 0;

            return window.scrollY < 2 && lenisScroll < 2 && lenisTarget < 2 && savedScroll < 2;
        });
    }

    async function expectEncryptionInitialized(page, previousId = 0) {
        await page.waitForFunction((previousId) => {
            const canvas = document.querySelector('#enc-canvas');
            const active = window.__itsbagelbotPreload?.activeEncryption;
            return Boolean(
                canvas &&
                active?.id > previousId &&
                active.section === document.querySelector('#enc-section') &&
                canvas.clientWidth > 0 &&
                canvas.clientHeight > 0 &&
                canvas.width >= canvas.clientWidth &&
                canvas.height >= canvas.clientHeight &&
                (canvas.width !== 300 || canvas.height !== 150)
            );
        }, previousId);
    }

    test('home renders hero + Act II sections', async ({ page }) => {
        await page.goto('/');

        await expect(page.locator('h1').first()).toContainText('Your Stream');

        // Encryption act is untouched
        await expect(page.locator('#enc-section')).toHaveCount(1);

        // Sparse pockets reuse the inner-page mote field without covering home.
        await expect(page.locator('.home-light-field[data-field]')).toHaveCount(2);
        await expect(page.locator('#safety-layers .home-light-field')).toHaveCount(1);
        await expect(page.locator('#how .home-light-field')).toHaveCount(1);

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
        await expect(quiet.locator('[data-card]')).toHaveCount(5);
        await expect(quiet).toContainText('It catches the noise before you do.');

        // Four-layer safety pipeline
        const safety = page.locator('#safety-layers');
        await expect(safety.locator('[data-card]')).toHaveCount(3);
        await expect(safety).toContainText('A classifier jury, not one guess.');
        await expect(safety).toContainText('One raid warns every protected channel.');

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

    test('static gates open in sync with the message', async ({ page }) => {
        await page.goto('/');

        const samples = await page.evaluate(() => {
            const lane = document.querySelector('.gates__lane');
            const packet = document.querySelector('.gates__packet');
            const gates = [...document.querySelectorAll('.gates__gate i')];
            if (!lane || !packet || gates.length !== 3) return [];

            const animated = [packet, ...gates];
            const animations = animated.map((element) => element.getAnimations()[0]);
            animations.forEach((animation) => animation.pause());

            return [1680, 2580, 3480].map((time) => {
                animations.forEach((animation) => { animation.currentTime = time; });
                const laneRect = lane.getBoundingClientRect();
                const packetRect = packet.getBoundingClientRect();
                return {
                    packet: (packetRect.left - laneRect.left) / laneRect.width,
                    gates: gates.map((gate) => gate.getBoundingClientRect().height),
                };
            });
        });

        expect(samples).toHaveLength(3);
        for (const [index, sample] of samples.entries()) {
            expect(sample.packet).toBeGreaterThan([0.2, 0.4, 0.6][index]);
            expect(sample.packet).toBeLessThan([0.38, 0.58, 0.8][index]);
            expect(sample.gates[index]).toBeLessThan(25);
        }
    });

    test('contact renders 4 switchboard lines with copy affordance', async ({ page }) => {
        await page.goto('/contact');

        await expect(page.locator('.phero__title')).toContainText('Talk to a human.');
        await expect(page.locator('.board .line')).toHaveCount(4);
        await expect(page.locator('.line').filter({ hasText: 'Discord' })).toHaveCount(1);
        await expect(page.locator('.line').filter({ hasText: 'Support' })).toHaveCount(1);

        // Email lines expose a click-to-copy control
        await expect(page.locator('[data-copy]')).toHaveCount(2);
        await expect(page.locator('a button')).toHaveCount(0);
        await page.locator('[data-copy]').first().click();
        await expect(page).toHaveURL(/\/contact\/?$/);
    });

    test('production assets referenced by the document are emitted', async ({ page, request }) => {
        await page.goto('/');

        const favicon = await page.locator('link[rel="icon"][type="image/png"]').first().getAttribute('href');
        expect(favicon).toBeTruthy();
        const response = await request.get(favicon);
        expect(response.ok()).toBeTruthy();
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
        await expect(page.locator('nav a[aria-label="Pricing"].is-active')).toHaveCount(1);
    });

    test('client route changes always start at the top', async ({ page }) => {
        await page.goto('/');
        await jumpDown(page);

        await page.locator('nav a[aria-label="Pricing"]').click();
        await expect(page).toHaveURL(/\/pricing\/?$/);
        await expectPageTop(page);

        await jumpDown(page);
        await page.locator('nav a[aria-label="Contact"]').click();
        await expect(page).toHaveURL(/\/contact\/?$/);
        await expectPageTop(page);

        await page.goBack();
        await expect(page).toHaveURL(/\/pricing\/?$/);
        await expectPageTop(page);
    });

    test('decode text animates after client route swaps', async ({ page }) => {
        await page.goto('/');

        await page.locator('nav a[aria-label="Pricing"]').click();
        await expect(page).toHaveURL(/\/pricing\/?$/);

        await page.waitForFunction(() => {
            const title = document.querySelector('.phero__title');
            return Boolean(
                title &&
                title.dataset.decodeReady === 'true' &&
                title.dataset.decode &&
                title.textContent !== title.dataset.decode
            );
        });

        await expect(page.locator('.phero__title')).toContainText('Free is the whole product.', { timeout: 3000 });
    });

    test('encryption scene boots again when returning home', async ({ page }) => {
        await page.goto('/');

        await expectEncryptionInitialized(page);

        const firstSceneId = await page.evaluate(() => window.__itsbagelbotPreload.activeEncryption.id);

        await page.locator('nav a[aria-label="Pricing"]').click();
        await expect(page).toHaveURL(/\/pricing\/?$/);
        await expect(page.locator('#enc-canvas')).toHaveCount(0);

        await page.goBack();
        await page.waitForFunction(() => location.pathname === '/');
        await expectPageTop(page);

        await expectEncryptionInitialized(page, firstSceneId);
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
