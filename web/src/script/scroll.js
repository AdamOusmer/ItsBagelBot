import Lenis from 'lenis';

const lenis = new Lenis({
    lerp: 0.1,           // replaces duration + easing (0–1, lower = smoother)
    smoothWheel: true,   // replaces smooth
    syncTouch: false,    // replaces smoothTouch
});

const root = document.documentElement;
let viewportHeight = Math.max(1, window.innerHeight);
let lastHeroProgress = -1;
let lastHeroUiHidden = null;
let lastHeroComplete = null;
let rafId = 0;

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

function raf(time) {
    lenis.raf(time);
    updateHeroProgress();
    rafId = requestAnimationFrame(raf);
}

function startRaf() {
    if (rafId) return;
    rafId = requestAnimationFrame(raf);
}

function stopRaf() {
    if (!rafId) return;
    cancelAnimationFrame(rafId);
    rafId = 0;
}

function onVisibilityChange() {
    if (document.hidden) {
        stopRaf();
    } else {
        updateViewportHeight();
        updateHeroProgress();
        startRaf();
    }
}

window.addEventListener('resize', updateViewportHeight, {passive: true});
document.addEventListener('visibilitychange', onVisibilityChange);
updateHeroProgress();
startRaf();
