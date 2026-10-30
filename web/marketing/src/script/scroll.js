// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createSmoothScroll } from '@bagel/ui/lib/lenis';
import { subscribe, wake } from '@bagel/ui/lib/raf-loop';
import { getLangSwitchScrollY } from '../i18n/route-transition';

const smooth = createSmoothScroll();

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

// Kept off :root, where a per-frame custom property restyles every element on the page.
let hero = document.querySelector('[data-hero-progress]');
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

const HERO_COMPLETE_HINTS = [
    ['--hero-animation-state', 'paused', 'running'],
    ['--hero-content-will-change', 'auto', 'opacity, transform, filter'],
    ['--hero-orb-will-change', 'auto', 'opacity, transform'],
    ['--hero-decor-display', 'none', 'block'],
];

function applyHeroComplete(complete) {
    for (const [name, done, live] of HERO_COMPLETE_HINTS) {
        hero.style.setProperty(name, complete ? done : live);
    }
}

function updateHeroProgress() {
    if (!hero) return;
    const heroProgress = Math.min(1, Math.max(0, window.scrollY / viewportHeight));
    const heroUiHidden = heroProgress >= 2 / 3;
    const heroComplete = heroProgress >= 1;

    if (Math.abs(heroProgress - lastHeroProgress) > 0.0005) {
        hero.style.setProperty('--hero-p', heroProgress.toFixed(4));
        lastHeroProgress = heroProgress;
    }

    if (heroUiHidden !== lastHeroUiHidden) {
        hero.style.setProperty('--hero-ui-pointer-events', heroUiHidden ? 'none' : 'auto');
        lastHeroUiHidden = heroUiHidden;
        hero.dispatchEvent(new CustomEvent('itsbagelbot:hero-fade', { detail: { faded: heroUiHidden } }));
    }

    if (heroComplete !== lastHeroComplete) {
        applyHeroComplete(heroComplete);
        lastHeroComplete = heroComplete;
    }
}

function heroTick() {
    updateHeroProgress();
    return false;
}

const wakeHero = () => wake(heroTick);
window.addEventListener('scroll', wakeHero, {passive: true});
scrollController.on('scroll', wakeHero);

function startHero() {
    if (unsubscribeHero) return;
    unsubscribeHero = subscribe(heroTick);
}

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

function clampScroll(top) {
    const max = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
    return Math.min(Math.max(0, top), max);
}

function restoreRouteScroll(top) {
    const clamped = clampScroll(top);
    scrollController.resize?.();
    scrollController.scrollTo(clamped, {immediate: true, force: true});
    window.scrollTo({left: 0, top: clamped, behavior: 'instant'});
    stampCurrentHistoryPosition(0, clamped);
    updateViewportHeight();
    updateHeroProgress();
}

// The swap brings a new hero without inline style, so the cached values no longer match what is on the page.
function forgetHeroState() {
    hero = document.querySelector('[data-hero-progress]');
    lastHeroProgress = -1;
    lastHeroUiHidden = null;
    lastHeroComplete = null;
}

let traversedTop = null;
let routeFrame = 0;

// Read when the navigation starts: a scrollend still pending from the last restore lands on the
// new history entry and Astro saves it there. A follow-up frame left over from the previous swap
// would overwrite the saved position the same way. Astro saves on scrollend, which a mid-scroll
// click beats.
document.addEventListener('astro:before-preparation', (event) => {
    cancelAnimationFrame(routeFrame);
    const top = history.state?.scrollY;
    const traversing = event.navigationType === 'traverse';
    traversedTop = traversing && Number.isFinite(top) ? top : null;
    if (!traversing) stampCurrentHistoryPosition(window.scrollX, window.scrollY);
});

let scrolledSinceSwap = false;

for (const type of ['wheel', 'touchstart', 'keydown', 'pointerdown']) {
    window.addEventListener(type, () => { scrolledSinceSwap = true; }, {passive: true, capture: true});
}

// The follow-up frame can land late on a busy main thread, after the reader already moved.
function settleRouteScroll(settle) {
    settle();
    scrolledSinceSwap = false;
    routeFrame = requestAnimationFrame(() => {
        if (!scrolledSinceSwap) settle();
        scrollController.resize?.();
    });
}

function resetRouteScroll() {
    forgetHeroState();
    const keptScrollY = getLangSwitchScrollY() ?? traversedTop;
    if (keptScrollY !== null) settleRouteScroll(() => restoreRouteScroll(keptScrollY));
    else settleRouteScroll(jumpToPageTop);
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
