import { test, expect } from '@playwright/test';

/**
 * Encryption scene: the preview_* harness runs pages hidden, so rAF +
 * IntersectionObserver never fire there and the WebGL scene / decode scramble
 * can't be exercised. Playwright drives a real (visible) Chromium where both
 * run, so this is where we prove the animation + WebGL actually fire, plus the
 * client-side nav re-init and scroll reset.
 */

// Force the intro loader "done" and wait for the scene to build. Init is
// gated behind boot-complete + IntersectionObserver + a post-boot timer, all
// of which need a real browser. A resized canvas (> the 300x150 default) means
// renderer.setSize ran, i.e. the WebGL renderer was created.
async function bootAndInit(page) {
    await page.evaluate(() => document.dispatchEvent(new CustomEvent('itsbagelbot:loader-complete')));
    await expect
        .poll(
            () =>
                page.evaluate(() => {
                    const c = document.getElementById('enc-canvas');
                    const reg = window.__itsbagelbotPreload;
                    return Boolean(reg && reg.encryptionData) && Boolean(c) && c.width > 300;
                }),
            { timeout: 20_000, message: 'encryption WebGL scene should initialize' },
        )
        .toBe(true);
}

test.describe('encryption scene', () => {
    test('requestAnimationFrame fires (animation loop can run)', async ({ page }) => {
        await page.goto('/');
        const framesFired = await page.evaluate(
            () =>
                new Promise((resolve) => {
                    let n = 0;
                    const tick = () => {
                        n += 1;
                        n < 3 ? requestAnimationFrame(tick) : resolve(n);
                    };
                    requestAnimationFrame(tick);
                    setTimeout(() => resolve(n), 2000);
                }),
        );
        // In the hidden preview harness this is 0; a real browser gives >= 3.
        expect(framesFired).toBeGreaterThanOrEqual(3);
    });

    test('WebGL renderer initializes and a live context is attached', async ({ page }) => {
        test.slow(); // software WebGL under parallel workers is CPU-heavy
        await page.goto('/');
        await bootAndInit(page);

        const webgl = await page.evaluate(() => {
            const c = document.getElementById('enc-canvas');
            // three built the context as webgl2; getContext returns the same one.
            const ctx = c.getContext('webgl2') || c.getContext('webgl');
            return { hasContext: Boolean(ctx), lost: ctx ? ctx.isContextLost() : true };
        });
        expect(webgl.hasContext).toBe(true);
        expect(webgl.lost).toBe(false);
    });

    test('scroll drives the animation (--hero-p advances)', async ({ page }) => {
        test.slow();
        await page.goto('/');
        await bootAndInit(page);

        const readHeroP = () =>
            page.evaluate(
                () => parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--hero-p')) || 0,
            );
        const before = await readHeroP();

        // Jump scroll deterministically; the rAF loop reads scrollY each frame
        // and advances --hero-p. (Wheel easing is too timing-sensitive under
        // parallel WebGL contention.)
        await page.evaluate(() => {
            const y = window.innerHeight * 2;
            window.lenis && typeof window.lenis.scrollTo === 'function'
                ? window.lenis.scrollTo(y, { immediate: true })
                : window.scrollTo(0, y);
        });

        await expect
            .poll(readHeroP, { timeout: 15_000, message: '--hero-p should advance as the rAF scroll loop runs' })
            .toBeGreaterThan(before);
    });

    test('heading decode reveal fires (shared decode.js scramble)', async ({ page }) => {
        test.slow();
        await page.goto('/');
        await bootAndInit(page);

        // Watch the first overlay heading for text mutations before it appears.
        await page.evaluate(() => {
            const h = document.querySelector('#enc-ov-0 .enc-h2');
            window.__decodeSeen = [];
            window.__decodeOriginal = h.textContent;
            const mo = new MutationObserver(() => window.__decodeSeen.push(h.textContent));
            mo.observe(h, { childList: true, characterData: true, subtree: true });
        });

        // Scroll to the first node's progress (~0.12) so overlay 0 fades in and
        // its decode fires. Uses the same scroll metrics the scene uses.
        await page.evaluate(() => {
            const s = document.getElementById('enc-section');
            const heroOffset = window.innerHeight;
            const top = s.offsetTop + heroOffset;
            const total = Math.max(1, s.offsetHeight - window.innerHeight - heroOffset);
            const y = top + total * 0.12;
            window.lenis && typeof window.lenis.scrollTo === 'function'
                ? window.lenis.scrollTo(y, { immediate: true })
                : window.scrollTo(0, y);
        });

        await expect
            .poll(
                () =>
                    page.evaluate(() =>
                        (window.__decodeSeen || []).some((v) => v !== window.__decodeOriginal),
                    ),
                { timeout: 15_000, message: 'heading should scramble (decode) as the overlay appears' },
            )
            .toBe(true);

        // ...and resolve back to the real title once the scramble finishes.
        await expect
            .poll(() => page.evaluate(() => document.querySelector('#enc-ov-0 .enc-h2').textContent), {
                timeout: 15_000,
            })
            .toBe(await page.evaluate(() => window.__decodeOriginal));
    });

    test('re-initializes and scroll resets after client-side nav away and back', async ({ page }) => {
        test.slow();
        await page.goto('/');
        await bootAndInit(page);

        // Scroll down into the section.
        await page.evaluate(() => {
            window.lenis && typeof window.lenis.scrollTo === 'function'
                ? window.lenis.scrollTo(2200, { immediate: true })
                : window.scrollTo(0, 2200);
        });
        await expect
            .poll(() => page.evaluate(() => Math.round(window.scrollY)))
            .toBeGreaterThan(300);

        // Client-side nav away (ClientRouter view transition) then back.
        await page.locator('nav a[href="/pricing"]').first().click();
        await page.waitForURL('**/pricing');
        await page.locator('nav a[href="/"]').first().click();
        await page.waitForURL((url) => url.pathname === '/');

        // Scroll must reset to the top (the reported bug).
        await expect
            .poll(() => page.evaluate(() => Math.round(window.scrollY)), {
                timeout: 15_000,
                message: 'scroll should reset to top on navigation back',
            })
            .toBeLessThan(50);

        // setup() must re-run on astro:page-load (scroll assist re-registered)...
        await expect
            .poll(() => page.evaluate(() => typeof window.__encryptionScrollAssist === 'function'), {
                timeout: 10_000,
            })
            .toBe(true);

        // ...and the WebGL scene must rebuild on the fresh canvas (re-sized).
        await expect
            .poll(
                () =>
                    page.evaluate(() => {
                        const c = document.getElementById('enc-canvas');
                        return Boolean(c) && c.width > 300;
                    }),
                { timeout: 20_000, message: 'scene should rebuild without a hard reload' },
            )
            .toBe(true);
    });
});
