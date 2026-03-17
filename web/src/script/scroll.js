import Lenis from 'lenis';

const lenis = new Lenis({
    lerp: 0.1,           // replaces duration + easing (0–1, lower = smoother)
    smoothWheel: true,   // replaces smooth
    syncTouch: false,    // replaces smoothTouch
});

function raf(time) {
    lenis.raf(time);
    requestAnimationFrame(raf);
}

requestAnimationFrame(raf);