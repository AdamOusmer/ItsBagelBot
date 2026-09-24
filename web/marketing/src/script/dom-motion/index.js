// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {finePointer, reduceMotion} from "@bagel/ui/lib/motion-query";
import {subscribe, wake} from "@bagel/ui/lib/raf-loop";

import {Pointer} from "./pointer.js";
import {Parallax} from "./parallax.js";

const INSTANCE_KEY = "__itsBagelBotDomMotion";
const STYLE_ID = "itsbagelbot-dom-motion-style";
const ACTIVE_CLASS = "is-dom-motion-active";

const POINTER_EASE = 0.07;

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
