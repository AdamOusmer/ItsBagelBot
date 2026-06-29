/**
 * Scroll reveal — the shared entrance for every section.
 *
 * Any element tagged `data-reveal` starts hidden (opacity 0 + a small
 * translate, defined globally in style.css) and transitions in the first time
 * it scrolls into view. Stagger neighbours with inline `style="--reveal-i: N"`.
 *
 * This replaces the old `animation: fadeUp ... forwards` pattern, which fired
 * on page load even for sections far below the fold. Reveal is viewport-driven,
 * so the choreography matches the hero. Works on every device (not gated on a
 * fine pointer like the parallax system) and respects reduced-motion.
 */

const SELECTOR = "[data-reveal]";
const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

function revealAll() {
    document.querySelectorAll(SELECTOR).forEach((el) => el.classList.add("is-revealed"));
}

let observer = null;

function getObserver() {
    if (observer) return observer;

    observer = new IntersectionObserver(
        (entries, obs) => {
            entries.forEach((entry) => {
                if (!entry.isIntersecting) return;
                entry.target.classList.add("is-revealed");
                if (entry.target.dataset.revealRepeat !== "true") {
                    obs.unobserve(entry.target);
                }
            });
        },
        { rootMargin: "0px 0px -10% 0px", threshold: 0.08 },
    );

    return observer;
}

function setup() {
    if (reduceMotion.matches || !("IntersectionObserver" in window)) {
        revealAll();
        return;
    }

    const io = getObserver();

    document.querySelectorAll(SELECTOR).forEach((el) => {
        if (el.dataset.revealReady === "true") return;
        el.dataset.revealReady = "true";

        // Already on screen at load (above the fold): reveal immediately so it
        // plays its entrance, instead of waiting for a scroll that never comes.
        const rect = el.getBoundingClientRect();
        if (rect.top < window.innerHeight && rect.bottom > 0) {
            el.classList.add("is-revealed");
            return;
        }

        io.observe(el);
    });
}

setup();
document.addEventListener("astro:page-load", setup);
