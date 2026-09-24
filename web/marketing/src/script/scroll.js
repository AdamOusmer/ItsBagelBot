// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createSmoothScroll } from '@bagel/ui/lib/lenis';
import { subscribe, wake } from '@bagel/ui/lib/raf-loop';

function applyVirtualScrollAssist(data) {
    const assist = window.__encryptionScrollAssist;
    if (typeof assist !== 'function') return true;

    return assist(data) !== false;
}

const smooth = createSmoothScroll({virtualScroll: applyVirtualScrollAssist});

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

const HERO_COMPLETE_HINTS = [
    ['--hero-animation-state', 'paused', 'running'],
    ['--hero-content-will-change', 'auto', 'opacity, transform, filter'],
    ['--hero-orb-will-change', 'auto', 'opacity, transform'],
    ['--hero-decor-display', 'none', 'block'],
];

function applyHeroComplete(complete) {
    for (const [name, done, live] of HERO_COMPLETE_HINTS) {
        root.style.setProperty(name, complete ? done : live);
    }
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
        applyHeroComplete(heroComplete);
        lastHeroComplete = heroComplete;
    }
}

function heroTick() {
    updateHeroProgress();
}

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
