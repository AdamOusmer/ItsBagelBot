// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The marketing site's scroll layer: the shared smooth scroller, plus the hero
 * progress variables that only this site has.
 *
 * The Lenis construction, the `window.__lenis` publication and the rAF tick all
 * left this file for `@bagel/ui/lib/lenis` and `@bagel/ui/lib/raf-loop`. Three
 * sites were initialising Lenis with the same three options and running three
 * private rAF loops; on this page there were four such loops (this one, the
 * cursor, dom-motion and the mote field) firing every frame.
 *
 * What stays here is genuinely site-specific: `--hero-p` and the two paint
 * hints derived from it, which the hero scene, the scroll cue and the orbs read
 * from CSS. Those are written from the shared tick, so the variable and the
 * scroll position can never be a frame apart.
 */

import { createSmoothScroll } from '@bagel/ui/lib/lenis';
import { subscribe, wake } from '@bagel/ui/lib/raf-loop';

function applyVirtualScrollAssist(data) {
    const assist = window.__encryptionScrollAssist;
    if (typeof assist !== 'function') return true;

    return assist(data) !== false;
}

const smooth = createSmoothScroll({virtualScroll: applyVirtualScrollAssist});

/**
 * Native-scroll stand-in for the encryption scene.
 *
 * `createSmoothScroll` returns null under reduced motion — correctly: there is
 * no smooth scroller to hand out, and that guard is new here (this file used to
 * construct Lenis unconditionally). But the encryption scene is not a motion
 * effect that can simply be skipped. It is a whole section that reads scroll
 * position, velocity and snap state to drive its geometry, and it already has
 * its own reduced-motion handling: it collapses its snap durations rather than
 * disappearing. Handing it `null` would blank the section for exactly the
 * visitors who asked for less movement, which is a far worse outcome than a
 * less clever snap.
 *
 * So it gets a controller of the same shape backed by the browser's own
 * scrolling. `on`/`off` are no-ops on purpose: the only events the scene
 * subscribes to are Lenis's virtual-scroll events, which do not exist without
 * Lenis, and its snap code already handles never receiving one.
 */
const nativeScroll = {
    get scroll() {
        return window.scrollY;
    },
    get targetScroll() {
        return window.scrollY;
    },
    get velocity() {
        return 0;
    },
    get isScrolling() {
        return false;
    },
    scrollTo(target, options = {}) {
        window.scrollTo({
            left: 0,
            top: target,
            behavior: options.immediate ? 'instant' : 'smooth',
        });
    },
    resize() {},
    on() {},
    off() {},
};

const scrollController = smooth?.lenis ?? nativeScroll;

const root = document.documentElement;
let viewportHeight = Math.max(1, window.innerHeight);
let lastHeroProgress = -1;
let lastHeroUiHidden = null;
let lastHeroComplete = null;
let unsubscribeHero = null;

function loaderOwnsViewport() {
    const loader = document.getElementById('loader-wrapper');
    if (!loader) return false;
    if (loader.getAttribute('aria-hidden') === 'true') return false;
    return loader.style.display !== 'none';
}

function updateViewportHeight() {
    viewportHeight = Math.max(1, window.innerHeight);
}

function updateHeroProgress() {
    const heroProgress = Math.min(1, Math.max(0, window.scrollY / viewportHeight));
    const heroUiHidden = heroProgress >= 2 / 3;
    const heroComplete = heroProgress >= 1;

    if (Math.abs(heroProgress - lastHeroProgress) > 0.0005) {
        root.style.setProperty('--hero-p', heroProgress.toFixed(4));
        lastHeroProgress = heroProgress;
    }

    if (heroUiHidden !== lastHeroUiHidden) {
        root.style.setProperty('--hero-ui-pointer-events', heroUiHidden ? 'none' : 'auto');
        lastHeroUiHidden = heroUiHidden;
    }

    if (heroComplete !== lastHeroComplete) {
        root.style.setProperty('--hero-animation-state', heroComplete ? 'paused' : 'running');
        root.style.setProperty('--hero-content-will-change', heroComplete ? 'auto' : 'opacity, transform, filter');
        root.style.setProperty('--hero-orb-will-change', heroComplete ? 'auto' : 'opacity, transform');
        lastHeroComplete = heroComplete;
    }
}

// Returns nothing, which the shared scheduler reads as "tick me again": the
// hero variables track a scroll position that can change at any moment and
// there is no settle condition short of the page going away. The scheduler's
// own document.hidden gate replaces the visibilitychange start/stop pair this
// file used to run by hand.
function heroTick() {
    updateHeroProgress();
}

function startHero() {
    if (unsubscribeHero) return;
    unsubscribeHero = subscribe(heroTick);
}

// The boot loader owns the viewport until it fades, and the scroller must not
// interpret wheel events behind it. This used to fall out of the old design for
// free — one loop drove both Lenis and the hero variables, and the loader gate
// simply did not start it. Now that the scroller is ticked by the shared
// scheduler, holding it back is an explicit stop()/start() pair.
if (loaderOwnsViewport()) smooth?.lenis.stop();

function stampCurrentHistoryPosition(left = 0, top = 0) {
    if (!history.state) return;

    history.scrollRestoration = 'manual';
    history.replaceState({
        ...history.state,
        scrollX: left,
        scrollY: top,
    }, '');
}

function jumpToPageTop() {
    scrollController.resize?.();
    scrollController.scrollTo(0, {immediate: true, force: true});
    window.scrollTo({left: 0, top: 0, behavior: 'instant'});
    stampCurrentHistoryPosition(0, 0);
    updateViewportHeight();
    updateHeroProgress();
}

function resetRouteScroll() {
    jumpToPageTop();

    requestAnimationFrame(() => {
        jumpToPageTop();
        scrollController.resize?.();
    });
}

window.addEventListener('resize', () => {
    updateViewportHeight();
    wake();
}, {passive: true});

document.addEventListener('astro:after-swap', resetRouteScroll);
document.addEventListener('itsbagelbot:loader-complete', () => {
    updateViewportHeight();
    updateHeroProgress();
    smooth?.lenis.start();
    startHero();
}, {once: true});
updateHeroProgress();

if (!loaderOwnsViewport()) {
    startHero();
}

export default scrollController;
