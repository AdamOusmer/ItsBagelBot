// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * DomMotion: cursor-driven motion system.
 *
 * Composes two subsystems that share a single Pointer and a single
 * requestAnimationFrame loop:
 *
 *   Parallax: header/footer + ornaments drift against the cursor (header
 *              retains a soft 3D tilt, footer only drifts).
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

import {finePointer, reduceMotion} from "@bagel/ui/lib/motion-query";
import {subscribe, wake} from "@bagel/ui/lib/raf-loop";

import {Pointer} from "./pointer.js";
import {Parallax} from "./parallax.js";

const INSTANCE_KEY = "__itsBagelBotDomMotion";
const STYLE_ID = "itsbagelbot-dom-motion-style";
const ACTIVE_CLASS = "is-dom-motion-active";

const POINTER_EASE = 0.07;

/**
 * How long after the pointer stops moving the parallax keeps easing before the
 * shared scheduler is allowed to drop it. The pointer eases at 0.07 per frame,
 * which is slow on purpose (the drift is meant to read as the page having
 * weight), and at that rate the last visible tenth of a pixel of travel lands
 * roughly 40 frames after the final pointermove. Settling on distance alone cut
 * the tail off visibly; 700ms outlasts it with room, and the cost of being
 * wrong in this direction is a handful of no-op frames rather than a visible
 * stop.
 */
const SETTLE_DELAY_MS = 700;

const MOTION_CSS = `
@media (prefers-reduced-motion: no-preference) and (hover: hover) and (pointer: fine) {
    :root.${ACTIVE_CLASS} body > header {
        translate: var(--dom-motion-page-x, 0px) var(--dom-motion-page-y, 0px);
        transform: perspective(1800px)
            rotateX(var(--dom-motion-page-tilt-x, 0deg))
            rotateY(var(--dom-motion-page-tilt-y, 0deg));
        transform-origin: center center;
        will-change: translate, transform;
    }

    :root.${ACTIVE_CLASS} body > footer {
        translate: var(--dom-motion-page-x, 0px) var(--dom-motion-page-y, 0px);
        will-change: translate;
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
    const cleanupCallbacks = [];

    const pointer = new Pointer();
    const parallax = new Parallax(pointer, root);

    let active = false;
    let unsubscribe = null;

    function canAnimate() {
        return finePointer.matches && !reduceMotion.matches && !document.hidden;
    }

    // Ticked by @bagel/ui's shared scheduler. The settle protocol is the same
    // one this file invented and the reason it exists: return false and the
    // scheduler drops this subscriber, and once every subscriber has settled
    // the page schedules no frames at all. The difference is that the parallax
    // now settles into the SAME loop the mote field and the smooth scroller
    // run in, rather than being one of four.
    function tick(now) {
        if (!active || !canAnimate()) return false;

        pointer.step(POINTER_EASE);
        parallax.apply();

        return !(pointer.isSettled() && now - pointer.lastMoveAt > SETTLE_DELAY_MS);
    }

    function start() {
        if (!active) return;
        if (unsubscribe) wake(tick);
        else unsubscribe = subscribe(tick);
    }

    function stop() {
        if (!unsubscribe) return;
        unsubscribe();
        unsubscribe = null;
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
        onMediaChange(reduceMotion, syncState),
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
