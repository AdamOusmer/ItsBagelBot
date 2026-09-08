// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * bindOnce: the one place the site's "wire an interactive island" rule lives.
 *
 * The site runs Astro's ClientRouter, so an island can arrive by a full page
 * load OR by a DOM swap on navigation. Wiring on `astro:page-load` covers both
 * cases, and the `data-ready` flag stops the second pass from binding a root
 * that survived the swap twice over (double listeners, double counters).
 *
 * The flag is a DOM attribute rather than a WeakSet of bound roots on purpose:
 * an attribute is observable from outside the bundle, so the Playwright suite
 * can await `[data-builder][data-ready="1"]` instead of racing a timeout
 * against chunk download. A WeakSet would be tidier and untestable.
 *
 * Nothing here unbinds: every listener these setups add is either on the root
 * itself (collected with it on swap) or on `window` for the life of the tab.
 * A widget that owns a rAF loop or an observer needs its own teardown on
 * `astro:before-swap` (see script/lightfield.js) and does not belong here.
 */
export function bindOnce(selector: string, setup: (root: HTMLElement) => void): void {
    function bindAll() {
        document.querySelectorAll<HTMLElement>(selector).forEach((root) => {
            if (root.dataset.ready === '1') return;
            root.dataset.ready = '1';
            setup(root);
        });
    }

    bindAll();
    document.addEventListener('astro:page-load', bindAll);
}
