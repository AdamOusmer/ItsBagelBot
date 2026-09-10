// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// The changelog page renders one entry per file in src/content/changelog,
// newest version first. Deriving the expectation from those files (rather than
// hard-coding the release list) keeps the ordering assertion honest without
// making every release a test edit: the property under test is the ORDER, and
// the comparator here is written independently of the page's own.
const CHANGELOG_DIR = fileURLToPath(new URL('../src/content/changelog', import.meta.url));
const PRERELEASE_RANK = { alpha: 0, beta: 1, prerelease: 2 };

function versionKey(version) {
    const [core, pre] = version.replace(/^v/, '').split('-');
    const parts = core.split('.').map(Number);
    return [...parts, pre ? PRERELEASE_RANK[pre] ?? 3 : 4];
}

function releasesNewestFirst() {
    return readdirSync(CHANGELOG_DIR)
        .filter((f) => f.endsWith('.json'))
        .map((f) => JSON.parse(readFileSync(`${CHANGELOG_DIR}/${f}`, 'utf8')))
        .sort((a, b) => {
            const [x, y] = [versionKey(b.version), versionKey(a.version)];
            for (let i = 0; i < Math.max(x.length, y.length); i += 1) {
                if ((x[i] ?? 0) !== (y[i] ?? 0)) return (x[i] ?? 0) - (y[i] ?? 0);
            }
            return 0;
        });
}

test.describe('ItsBagelBot site', () => {
    async function jumpDown(page) {
        await page.evaluate(() => {
            const maxScroll = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
            const target = Math.min(Math.max(window.innerHeight * 1.4, 700), maxScroll);
            window.__lenis?.scrollTo?.(target, { immediate: true, force: true });
            window.scrollTo({ top: target, behavior: 'instant' });
        });
        await page.waitForFunction(() => window.scrollY > 500);
    }

    async function expectPageTop(page) {
        await page.waitForFunction(() => {
            const lenis = window.__lenis;
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
        // Three fields: the hero starfield plus the two section pockets.
        //
        // `.bb-light-field--bleed`, not the old `.home-light-field`: the field
        // moved to @bagel/ui/astro/LightField.astro and the full-bleed variant
        // that HomeLightField.astro existed to provide is now a modifier class.
        await expect(page.locator('.bb-light-field--bleed[data-field]')).toHaveCount(3);
        await expect(page.locator('.starfield .bb-light-field')).toHaveCount(1);
        await expect(page.locator('#safety-layers .bb-light-field')).toHaveCount(1);
        await expect(page.locator('#how .bb-light-field')).toHaveCount(1);

        // Playground: chat window, command chips, spam button, live feed seed
        const play = page.locator('#playground');
        await expect(play.locator('h2')).toContainText('Try it here.');
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
        await expect(quiet).toContainText('The things it handles while you stream.');
        // Song requests lead the bento: the full-width card is the first one.
        await expect(quiet.locator('[data-card]').first()).toContainText('Spotify song requests, run by chat.');

        // Four-layer safety pipeline
        const safety = page.locator('#safety-layers');
        await expect(safety.locator('[data-card]')).toHaveCount(3);
        await expect(safety).toContainText('Several classifiers have to agree.');
        await expect(safety).toContainText('One raid warns every protected channel.');

        // Steps
        const how = page.locator('#how');
        await expect(how.locator('.step')).toHaveCount(3);
        for (const step of ['Connect your channel.', 'Make it yours.', 'Go live, and breathe.']) {
            await expect(how).toContainText(step);
        }

        // Letter + finale
        await expect(page.locator('#letter')).toContainText('Nothing here is sold to advertisers');
        await expect(page.locator('[data-letter-stamp]')).toHaveCount(1);
        await expect(page.locator('.finale')).toContainText('tomorrow');

        // Ownership colophon names the account Hypixel verification looks for
        const about = page.locator('#about');
        await expect(about).toContainText('ItsMavey');
        const ignLinks = about.locator('a[href="https://namemc.com/profile/ItsMavey"]');
        await expect(ignLinks).toHaveCount(2);
    });

    test('Add to Twitch starts dashboard OAuth, not the console origin', async ({ page }) => {
        await page.goto('/');
        await expect(page.locator('.bb-nav a.bb-nav__cta')).toHaveAttribute(
            'href',
            'https://dashboard.itsbagelbot.com/auth/login'
        );
        await expect(page.locator('.finale__actions a').first()).toHaveAttribute(
            'href',
            'https://dashboard.itsbagelbot.com/auth/login'
        );

        await page.goto('/fr/');
        await expect(page.locator('.bb-nav a.bb-nav__cta')).toHaveAttribute(
            'href',
            'https://dashboard.itsbagelbot.com/auth/login?lang=fr'
        );
    });

    test('pricing renders free-first tiers, oath, and faq', async ({ page }) => {
        await page.goto('/pricing');

        await expect(page.locator('.bb-page-hero__title')).toContainText('Everything is on the free plan.');

        const tiers = page.locator('.tiers [data-card]');
        await expect(tiers).toHaveCount(3);
        await expect(tiers.nth(0)).toContainText('Free');
        await expect(tiers.nth(1)).toContainText('Premium');
        await expect(tiers.nth(2)).toContainText('Enterprise');

        // Premium framed as the tip jar, not the "recommended" upsell
        await expect(tiers.nth(1)).toContainText('The tip jar, with perks');

        await expect(page.locator('.tiers__oath')).toContainText('no card needed');

        await expect(page.locator('details')).toHaveCount(5);

        // Source available, never positioned as "open source"
        // (FAQ answer lives in a collapsed <details>, so assert presence, not visibility)
        await expect(page.getByText('not the same as open source')).toHaveCount(1);
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

            // The 7.2s gatePacket cycle holds the packet in front of each gate
            // while that gate opens: gate one 23-26% of the cycle, gate two
            // 45-48%, gate three 65-68% (SafetyLayers.astro keyframes). Sample
            // the midpoint of each hold/open overlap window, where the packet
            // is parked rather than mid-flight.
            return [1764, 3348, 4788].map((time) => {
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
            // Gates sit at 29%/50%/71% of the lane; the packet parks 46px short
            // of each, so the expected ratio is gate% minus 46px over the lane
            // width. Bounds allow the lane width to vary with the viewport.
            expect(sample.packet).toBeGreaterThan([0.14, 0.35, 0.56][index]);
            expect(sample.packet).toBeLessThan([0.26, 0.47, 0.68][index]);
            expect(sample.gates[index]).toBeLessThan(25);
        }
    });

    test('contact renders 4 switchboard lines with copy affordance', async ({ page }) => {
        await page.goto('/contact');

        await expect(page.locator('.bb-page-hero__title')).toContainText('Talk to a human.');
        await expect(page.locator('.board .line')).toHaveCount(4);
        await expect(page.locator('.line').filter({ hasText: 'Discord' })).toHaveCount(1);
        await expect(page.locator('.line').filter({ hasText: 'Support' })).toHaveCount(1);

        // Email lines expose a click-to-copy control
        await expect(page.locator('[data-copy]')).toHaveCount(2);
        await expect(page.locator('a button')).toHaveCount(0);
        await page.locator('[data-copy]').first().click();
        await expect(page).toHaveURL(/\/contact\/?$/);
    });

    test('changelog lists tagged releases ordered by version', async ({ page }) => {
        await page.goto('/changelog');

        await expect(page.locator('.bb-page-hero__title')).toContainText("What's new.");
        const releases = releasesNewestFirst();
        const items = page.locator('.clog__item');
        await expect(items).toHaveCount(releases.length);

        // Same publish day must not scramble order: newest version tag first.
        for (const [i, release] of releases.entries()) {
            await expect(items.nth(i).locator('.clog__ver')).toHaveText(release.version);
        }

        const newest = releases[0];
        const title = newest.title.en ?? newest.title;
        const highlights = newest.highlights.en ?? newest.highlights;
        await expect(items.first()).toContainText(title);
        await expect(items.first().locator(`.rtag--${newest.tag}`)).toHaveCount(1);
        await expect(items.first().locator('.clog__highlights li')).toHaveCount(highlights.length);
        await expect(items.first()).toContainText(highlights[0]);
        await expect(items.first().locator(`a[href="${newest.github}"]`)).toHaveCount(1);

        // The import wizard names every source it reads, so the release that
        // shipped it stays findable by the bot a streamer is leaving behind.
        const importer = items.filter({ hasText: 'Import wizard v1' });
        await expect(importer.locator('.clog__ver')).toHaveText('v0.1.2-beta');
        await expect(importer).toContainText('StreamElements, Moobot and Streamlabs Chatbot');
        await expect(items.filter({ hasText: 'Import from Nightbot' }).locator('.clog__ver')).toHaveText('v0.1.4-beta');

        await expect(items.last().locator('.rtag--alpha')).toHaveCount(1);

        // The footer's columns are @bagel/ui's `.bb-nav-link` now: the label is
        // the link's text, not an aria-label the old hand-written footer set.
        await expect(page.locator('footer a.bb-nav-link[href="/changelog/"]')).toHaveCount(1);

        await page.goto('/fr/changelog');
        await expect(page.locator('.bb-page-hero__title')).toContainText('Quoi de neuf.');
        await expect(page.locator('.clog__item').first()).toContainText(newest.title.fr ?? title);
        await expect(page.locator('.rtag--beta').first()).toContainText('Bêta');
        // The French entry names the same import sources as the English one.
        await expect(page.locator('.clog__item').filter({ hasText: "Assistant d'import v1" })).toContainText(
            'StreamElements, Moobot et Streamlabs Chatbot'
        );
        await expect(page.locator('.bb-lang-switch a[hreflang="en"]').first()).toHaveAttribute('href', '/changelog');
    });

    test('production assets referenced by the document are emitted', async ({ page, request }) => {
        await page.goto('/');

        await expect(page.locator('link[rel="icon"][type="image/svg+xml"]')).toHaveCount(0);
        const favicon = await page.locator('link[rel="icon"][sizes="32x32"]').getAttribute('href');
        expect(favicon).toBe('/favicon-32x32.png?v=bot-1');
        const response = await request.get(favicon);
        expect(response.ok()).toBeTruthy();
    });

    test('legal pages render with toc, plain words, and copy intact', async ({ page }) => {
        await page.goto('/privacy');
        await expect(page.locator('.bb-page-hero__title')).toContainText('Privacy Policy');
        await expect(page.locator('body')).toContainText('Data We Collect');
        await expect(page.locator('[data-legal-link]')).toHaveCount(11);
        await expect(page.locator('.lshell__plain').first()).toContainText('plain words');

        await page.goto('/terms');
        await expect(page.locator('.bb-page-hero__title')).toContainText('Terms of Service');
        await expect(page.locator('body')).toContainText('Acceptable Use');
        await expect(page.locator('body')).toContainText('Source Available License');
        await expect(page.locator('body')).toContainText('Third-Party Games and Trademarks');
        await expect(page.locator('[data-legal-link]')).toHaveCount(12);
    });

    test('active nav route is marked', async ({ page }) => {
        await page.goto('/pricing');
        await expect(page.locator('.bb-nav__links a.bb-nav-link[href="/pricing/"][aria-current="page"]')).toHaveCount(1);
    });

    test('client route changes always start at the top', async ({ page }) => {
        await page.goto('/');
        await jumpDown(page);

        await page.locator('.bb-nav__links a.bb-nav-link[href="/pricing/"]').click();
        await expect(page).toHaveURL(/\/pricing\/?$/);
        await expectPageTop(page);

        await jumpDown(page);
        await page.locator('.bb-nav__links a.bb-nav-link[href="/contact/"]').click();
        await expect(page).toHaveURL(/\/contact\/?$/);
        await expectPageTop(page);

        await page.goBack();
        await expect(page).toHaveURL(/\/pricing\/?$/);
        await expectPageTop(page);
    });

    test('decode text animates after client route swaps', async ({ page }) => {
        await page.goto('/');

        await page.locator('.bb-nav__links a.bb-nav-link[href="/pricing/"]').click();
        await expect(page).toHaveURL(/\/pricing\/?$/);

        // Mid-scramble: the element is showing something other than its final
        // text. `data-decode-ready` is gone -- the old script used it as an
        // idempotence flag, and @bagel/ui's observeDecode guards with
        // `observer.unobserve` and its own running set instead, so the
        // attribute was a marker with no reader. The condition below is the
        // same observable signal without it.
        await page.waitForFunction(() => {
            const title = document.querySelector('.bb-page-hero__title');
            return Boolean(
                title &&
                title.dataset.decode &&
                title.textContent !== title.dataset.decode
            );
        });

        await expect(page.locator('.bb-page-hero__title')).toContainText('Everything is on the free plan.', { timeout: 3000 });
    });

    test('encryption scene boots again when returning home', async ({ page }) => {
        await page.goto('/');

        await expectEncryptionInitialized(page);

        const firstSceneId = await page.evaluate(() => window.__itsbagelbotPreload.activeEncryption.id);

        await page.locator('.bb-nav__links a.bb-nav-link[href="/pricing/"]').click();
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

test.describe('guides & command builder', () => {
    test('guides hub lists the five guides and the builder tool', async ({ page }) => {
        await page.goto('/guides');

        await expect(page.locator('.bb-page-hero__title')).toContainText('Learn the bot.');
        await expect(page.locator('.gcard')).toHaveCount(5);
        await expect(page.locator('.gcard').first()).toHaveAttribute('href', '/guides/getting-started/');
        await expect(page.locator('.gcard').nth(2)).toHaveAttribute('href', '/guides/data-sources/');
        await expect(page.locator('.ghub__tool')).toHaveAttribute('href', '/command-builder');

        // The Guides nav entry is live and marked active.
        await expect(page.locator('.bb-nav__links a.bb-nav-link[href="/guides/"]')).toHaveAttribute('aria-current', 'page');
    });

    test('guide pages render toc, visuals, and pager', async ({ page }) => {
        await page.goto('/guides/commands');

        await expect(page.locator('[data-guide-link]')).toHaveCount(7);
        await expect(page.locator('[data-guide-section]')).toHaveCount(7);

        // Visual furniture: chat mock, dashboard frame, variable table.
        await expect(page.locator('.cmock__win').first()).toBeVisible();
        await expect(page.locator('.dframe__win')).toHaveCount(1);
        await expect(page.locator('#variables table td code').first()).toContainText('{user}');

        // Pager walks the handbook in both directions; data sources sits after commands.
        await expect(page.locator('.gshell__pager-card')).toHaveCount(2);
        await expect(page.locator('.gshell__pager-card--next')).toHaveAttribute('href', '/guides/data-sources/');
    });

    test('the response rehearsal expands tokens as you type', async ({ page }) => {
        await page.goto('/guides/commands');
        const widget = page.locator('[data-rehearsal]');
        await expect(widget).toBeVisible();

        await widget.locator('[data-rh-who]').fill('maya_live');
        await expect(widget.locator('[data-rh-output]')).toContainText('maya_live');
    });

    test('data sources guide teaches the path with a live picker', async ({ page }) => {
        await page.goto('/guides/data-sources');

        await expect(page.locator('[data-guide-section]')).toHaveCount(7);
        await expect(page.locator('.dframe__win')).toHaveCount(2);

        // Click a scalar leaf: the saved path and the token follow.
        const picker = page.locator('[data-pathpicker]');
        await picker.locator('[data-pp-tree] button:not([disabled])').first().click();
        await expect(picker.locator('[data-pp-token]')).toContainText('{urlfetch:');

        // The outcomes widget swaps the bot line to the exact fallback text.
        const outcomes = page.locator('[data-fetchoutcomes]');
        await outcomes.locator('[data-go-tab]').nth(2).click();
        await expect(outcomes.locator('[data-go-line]')).toContainText('[source timed out]');
    });

    test('module catalog filters by category', async ({ page }) => {
        await page.goto('/guides/modules');
        const catalog = page.locator('[data-mcat]');

        await expect(catalog.locator('.mcat__cell')).toHaveCount(24);
        const stats = catalog.locator('[data-mcat-chip]', { hasText: 'Stats' });
        await stats.click();
        await expect(stats).toHaveAttribute('aria-pressed', 'true');
        await expect(catalog.locator('.mcat__cell:visible')).toHaveCount(5);
    });

    test('french mirrors pair with english via the language switcher', async ({ page }) => {
        await page.goto('/fr/guides/commands');

        await expect(page.locator('.gshell__toc-label')).toHaveText('Dans ce guide');
        await expect(page.locator('.bb-lang-switch a[hreflang="en"]').first()).toHaveAttribute('href', '/guides/commands');

        await page.goto('/fr/guides/data-sources');
        await expect(page.locator('[data-guide-section]')).toHaveCount(7);
        await expect(page.locator('.bb-lang-switch a[hreflang="en"]').first()).toHaveAttribute('href', '/guides/data-sources');

        await page.goto('/guides');
        await expect(page.locator('.bb-lang-switch a[hreflang="fr"]').first()).toHaveAttribute('href', '/fr/guides/');
    });

    test('builder composes a command end to end', async ({ page }) => {
        await page.goto('/command-builder');
        await page.waitForSelector('[data-builder][data-ready="1"]');

        // Palette is scoped to what the bot actually expands for custom commands.
        const tokens = page.locator('[data-vars] .var code');
        await expect(tokens).toHaveCount(8);
        await expect(tokens.first()).toHaveText('{user}');

        await page.fill('[data-name]', 'greet');
        await page.fill('[data-template]', 'Hello ');
        // Clicking a variable inserts at the cursor and refreshes the rehearsal.
        await page.click('[data-vars] .var:first-child');
        await expect(page.locator('[data-output]')).toHaveText('!cmd add greet Hello {user}');
        // The rehearsal (ported dashboard ChatPreview) types for a beat, then
        // replies with the sample highlighted in a <mark>.
        await expect(page.locator('[data-chat] .line.bot .msg.reply')).toHaveText('Hello maya_live');
        await expect(page.locator('[data-chat] .line.bot .msg.reply mark')).toHaveText('maya_live');

        // The dashboard hand-off carries the whole draft.
        const href = await page.getAttribute('[data-send]', 'href');
        expect(href).toContain('dashboard.itsbagelbot.com/commands?compose=1');
        expect(href).toContain('name=greet');
        expect(href).toContain('perm=everyone');

        // Multi-line responses outgrow a single chat message: the !cmd copy
        // path steps aside, the dashboard path remains.
        await page.fill('[data-template]', 'line one\nline two');
        await expect(page.locator('[data-copy-wrap]')).toBeHidden();
        await expect(page.locator('[data-send-wrap]')).toBeVisible();
    });

    test('builder enforces the dashboard caps while typing', async ({ page }) => {
        await page.goto('/command-builder');
        await page.waitForSelector('[data-builder][data-ready="1"]');

        // 7 lines typed -> clamped to the service's 5; 600 chars -> 500.
        await page.fill('[data-template]', 'a\nb\nc\nd\ne\nf\ng');
        await expect(page.locator('[data-template]')).toHaveValue('a\nb\nc\nd\ne');
        await page.fill('[data-template]', 'x'.repeat(600));
        const len = await page.inputValue('[data-template]');
        expect(len.length).toBe(500);
    });

    test('french builder mirrors the english one', async ({ page }) => {
        await page.goto('/fr/command-builder');
        await page.waitForSelector('[data-builder][data-ready="1"]');

        await expect(page.locator('.bb-page-hero__title')).toContainText('Des commandes puissantes.');
        await expect(page.locator('[data-vars] .var code')).toHaveCount(8);
        const href = await page.getAttribute('[data-send]', 'href');
        expect(href).toContain('lang=fr');
    });

    test('builder module mode scopes the palette to the surface', async ({ page }) => {
        await page.goto('/command-builder');
        await page.waitForSelector('[data-builder][data-ready="1"]');

        await page.click('[data-mode="module"]');
        await page.selectOption('[data-surface]', 'shoutout');

        await expect(page.locator('[data-vars] .var code').first()).toHaveText('{raider}');
        await expect(page.locator('[data-send-wrap]')).toBeHidden();
        await expect(page.locator('[data-module-link]')).toHaveAttribute(
            'href',
            'https://dashboard.itsbagelbot.com/modules/shoutout'
        );
    });
});
