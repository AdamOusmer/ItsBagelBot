/**
 * DomMotion — cursor-driven motion system.
 *
 * Composes two subsystems that share a single Pointer and a single
 * requestAnimationFrame loop:
 *
 *   Parallax — header/footer + ornaments drift against the cursor with a
 *              soft 3D tilt.
 *
 * Top-level <section> and <main> are deliberately excluded from the
 * transform: applying `transform` or `translate` to them creates a new
 * containing block, which breaks `position: sticky` for their descendants
 * (the encryption scene relies on this).
 *
 * The system only runs on (hover: hover) + (pointer: fine) and respects
 * prefers-reduced-motion. The RAF loop self-suspends when motion settles
 * and resumes on the next pointer event.
 */

import {Pointer} from "./pointer.js";
import {Parallax} from "./parallax.js";

const INSTANCE_KEY = "__itsBagelBotDomMotion";
const STYLE_ID = "itsbagelbot-dom-motion-style";
const ACTIVE_CLASS = "is-dom-motion-active";

const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";
const FINE_POINTER_QUERY = "(hover: hover) and (pointer: fine)";

const POINTER_EASE = 0.07;
const SETTLE_DELAY_MS = 700;

const MOTION_CSS = `
@media (prefers-reduced-motion: no-preference) and (hover: hover) and (pointer: fine) {
    :root.${ACTIVE_CLASS} body > :where(header, footer) {
        translate: var(--dom-motion-page-x, 0px) var(--dom-motion-page-y, 0px);
        transform: perspective(1800px)
            rotateX(var(--dom-motion-page-tilt-x, 0deg))
            rotateY(var(--dom-motion-page-tilt-y, 0deg));
        transform-origin: center center;
        will-change: translate, transform;
    }

    :root.${ACTIVE_CLASS} body > .ornaments--page {
        translate: var(--dom-motion-frame-x, 0px) var(--dom-motion-frame-y, 0px);
        will-change: translate;
    }
}
`;

function installStyle() {
    if (document.getElementById(STYLE_ID)) return;

    const style = document.createElement("style");
    style.id = STYLE_ID;
    style.textContent = MOTION_CSS;
    document.head.appendChild(style);
}

function onMediaChange(mediaQueryList, callback) {
    mediaQueryList.addEventListener("change", callback);
    return () => mediaQueryList.removeEventListener("change", callback);
}

function createDomMotion() {
    const root = document.documentElement;
    const reducedMotion = window.matchMedia(REDUCED_MOTION_QUERY);
    const finePointer = window.matchMedia(FINE_POINTER_QUERY);
    const cleanupCallbacks = [];

    const pointer = new Pointer();
    const parallax = new Parallax(pointer, root);

    let active = false;
    let rafId = 0;

    function canAnimate() {
        return finePointer.matches && !reducedMotion.matches && !document.hidden;
    }

    function tick(now) {
        if (!active || !canAnimate()) {
            rafId = 0;
            return;
        }

        pointer.step(POINTER_EASE);
        parallax.apply();

        const settled =
            pointer.isSettled() && now - pointer.lastMoveAt > SETTLE_DELAY_MS;

        if (settled) {
            rafId = 0;
            return;
        }

        rafId = requestAnimationFrame(tick);
    }

    function start() {
        if (rafId || !active) return;
        rafId = requestAnimationFrame(tick);
    }

    function stop() {
        if (!rafId) return;
        cancelAnimationFrame(rafId);
        rafId = 0;
    }

    function activate() {
        if (active || !canAnimate()) return;

        active = true;
        root.classList.add(ACTIVE_CLASS);
        pointer.bind();
        start();
    }

    function deactivate() {
        if (!active) return;

        active = false;
        stop();
        pointer.unbind();
        parallax.reset();
        root.classList.remove(ACTIVE_CLASS);
    }

    function syncState() {
        if (canAnimate()) activate();
        else deactivate();
    }

    function destroy() {
        deactivate();
        cleanupCallbacks.forEach((cleanup) => cleanup());
        cleanupCallbacks.length = 0;
    }

    pointer.onActivity = start;
    installStyle();

    cleanupCallbacks.push(
        onMediaChange(reducedMotion, syncState),
        onMediaChange(finePointer, syncState),
    );

    document.addEventListener("visibilitychange", syncState);
    window.addEventListener("pagehide", destroy, {once: true});

    cleanupCallbacks.push(
        () => document.removeEventListener("visibilitychange", syncState),
        () => window.removeEventListener("pagehide", destroy),
    );

    syncState();

    return {destroy};
}

window[INSTANCE_KEY]?.destroy?.();
window[INSTANCE_KEY] = createDomMotion();
