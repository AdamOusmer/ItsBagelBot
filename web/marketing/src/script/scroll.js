// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { subscribe, wake } from '@bagel/ui/lib/raf-loop';
import { initLenis, onSmoothScroll } from '@bagel/ui/lib/smooth-scroll';
import { getLangSwitchScrollY } from '../i18n/route-transition';

let smooth = null;

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

const controller = () => smooth ?? nativeScroll;

// --hero-p is registered non-inherited and written on each reader: a per-frame write on the hero root restyled the whole crowd.
const HERO_PROGRESS_READERS = '.material, .starfield, .orb, .crowd .row';
let hero = document.querySelector('[data-hero-progress]');
let heroReaders = readersOf(hero);
let viewportHeight = Math.max(1, window.innerHeight);
let viewportWidth = window.innerWidth;
const touchOnly = window.matchMedia('(hover: none) and (pointer: coarse)');

function readersOf(root) {
    return root ? Array.from(root.querySelectorAll(HERO_PROGRESS_READERS)) : [];
}
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
    viewportWidth = window.innerWidth;
}

const HERO_COMPLETE_HINTS = [
    ['--hero-animation-state', 'paused', 'running'],
    ['--hero-content-will-change', 'auto', 'opacity, transform'],
    ['--hero-orb-will-change', 'auto', 'opacity, transform'],
    ['--hero-decor-visibility', 'hidden', 'visible'],
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
        const value = heroProgress.toFixed(4);
        for (const reader of heroReaders) reader.style.setProperty('--hero-p', value);
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

function startHero() {
    if (unsubscribeHero) return;
    unsubscribeHero = subscribe(heroTick);
}

onSmoothScroll((lenis) => {
    smooth = lenis;
    lenis.on('scroll', wakeHero);
    if (loaderOwnsViewport()) lenis.stop();
});

function mountSmoothScroll() {
    void initLenis();
}

if (document.readyState === 'complete') mountSmoothScroll();
else window.addEventListener('load', mountSmoothScroll, {once: true});

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
    controller().resize?.();
    controller().scrollTo(0, {immediate: true, force: true});
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
    controller().resize?.();
    controller().scrollTo(clamped, {immediate: true, force: true});
    window.scrollTo({left: 0, top: clamped, behavior: 'instant'});
    stampCurrentHistoryPosition(0, clamped);
    updateViewportHeight();
    updateHeroProgress();
}

// The swap brings a new hero without inline style, so the cached values no longer match what is on the page.
function forgetHeroState() {
    hero = document.querySelector('[data-hero-progress]');
    heroReaders = readersOf(hero);
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
    updateHeroProgress();
    scrolledSinceSwap = false;
    routeFrame = requestAnimationFrame(() => {
        if (!scrolledSinceSwap) settle();
        updateHeroProgress();
        controller().resize?.();
    });
}

function resetRouteScroll() {
    forgetHeroState();
    const keptScrollY = getLangSwitchScrollY() ?? traversedTop;
    if (keptScrollY !== null) settleRouteScroll(() => restoreRouteScroll(keptScrollY));
    else settleRouteScroll(jumpToPageTop);
}

// A height-only resize on a touch device is the address bar collapsing or expanding, one event per animation
// step; following it restyled the whole hero on every step.
window.addEventListener('resize', () => {
    if (touchOnly.matches && window.innerWidth === viewportWidth) return;
    updateViewportHeight();
    wakeHero();
}, {passive: true});

document.addEventListener('astro:after-swap', resetRouteScroll);

function refreshHero() {
    updateViewportHeight();
    updateHeroProgress();
}

// iOS Safari did not deliver a scroll event after the swap's jump to top, so the hero stayed at the previous
// page's progress until the first touch; the progress is written from scrollY directly after every settle and page load.
document.addEventListener('astro:page-load', refreshHero);
window.addEventListener('pageshow', refreshHero);
document.addEventListener('itsbagelbot:loader-complete', () => {
    updateViewportHeight();
    updateHeroProgress();
    smooth?.start();
    startHero();
}, {once: true});
updateHeroProgress();

if (!loaderOwnsViewport()) {
    startHero();
}

export default controller;
