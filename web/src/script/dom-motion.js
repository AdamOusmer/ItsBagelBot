const INSTANCE_KEY = "__itsBagelBotDomMotion";
const STYLE_ID = "itsbagelbot-dom-motion-style";
const ACTIVE_CLASS = "is-dom-motion-active";
const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";
const FINE_POINTER_QUERY = "(hover: hover) and (pointer: fine)";

const MAX_OFFSET_X = 4;
const MAX_OFFSET_Y = 3;
const EASE = 0.045;
const SETTLE_DISTANCE = 0.015;
const SETTLE_DELAY_MS = 700;

const MOTION_CSS = `
@media (prefers-reduced-motion: no-preference) and (hover: hover) and (pointer: fine) {
    :root.${ACTIVE_CLASS} body > :where(header, main, section, footer) {
        translate: var(--dom-motion-page-x, 0px) var(--dom-motion-page-y, 0px);
        transform: perspective(1800px) rotateX(var(--dom-motion-page-tilt-x, 0deg)) rotateY(var(--dom-motion-page-tilt-y, 0deg));
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
    if (typeof mediaQueryList.addEventListener === "function") {
        mediaQueryList.addEventListener("change", callback);
        return () => mediaQueryList.removeEventListener("change", callback);
    }

    mediaQueryList.addListener(callback);
    return () => mediaQueryList.removeListener(callback);
}

function clamp(value, min, max) {
    return Math.min(max, Math.max(min, value));
}

function createDomMotion() {
    const root = document.documentElement;
    const reducedMotion = window.matchMedia(REDUCED_MOTION_QUERY);
    const finePointer = window.matchMedia(FINE_POINTER_QUERY);
    const cleanupCallbacks = [];
    const lastValues = new Map();

    let viewportWidth = Math.max(1, window.innerWidth);
    let viewportHeight = Math.max(1, window.innerHeight);
    let active = false;
    let rafId = 0;
    let targetX = 0;
    let targetY = 0;
    let currentX = 0;
    let currentY = 0;
    let lastPointerX = viewportWidth / 2;
    let lastPointerY = viewportHeight / 2;
    let lastInputAt = 0;

    function canAnimate() {
        return finePointer.matches && !reducedMotion.matches && !document.hidden;
    }

    function setVar(name, value, unit, precision = 2) {
        const nextValue = `${value.toFixed(precision)}${unit}`;
        if (lastValues.get(name) === nextValue) return;

        root.style.setProperty(name, nextValue);
        lastValues.set(name, nextValue);
    }

    function writeMotionVars(x, y) {
        setVar("--dom-motion-page-x", x, "px");
        setVar("--dom-motion-page-y", y, "px");
        setVar("--dom-motion-page-tilt-x", y * -0.035, "deg", 3);
        setVar("--dom-motion-page-tilt-y", x * 0.04, "deg", 3);
        setVar("--dom-motion-frame-x", x * 0.22, "px");
        setVar("--dom-motion-frame-y", y * 0.22, "px");
    }

    function readViewport() {
        viewportWidth = Math.max(1, window.innerWidth);
        viewportHeight = Math.max(1, window.innerHeight);
    }

    function updateTargetFromPoint(clientX, clientY) {
        const nx = clamp(clientX / viewportWidth - 0.5, -0.5, 0.5) * 2;
        const ny = clamp(clientY / viewportHeight - 0.5, -0.5, 0.5) * 2;

        targetX = nx * -MAX_OFFSET_X;
        targetY = ny * -MAX_OFFSET_Y;
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

    function settleToCenter() {
        targetX = 0;
        targetY = 0;
        lastInputAt = performance.now();
        start();
    }

    function tick(now) {
        if (!active || !canAnimate()) {
            rafId = 0;
            return;
        }

        currentX += (targetX - currentX) * EASE;
        currentY += (targetY - currentY) * EASE;
        writeMotionVars(currentX, currentY);

        const isSettled =
            Math.abs(targetX - currentX) < SETTLE_DISTANCE &&
            Math.abs(targetY - currentY) < SETTLE_DISTANCE;

        if (isSettled && now - lastInputAt > SETTLE_DELAY_MS) {
            rafId = 0;
            return;
        }

        rafId = requestAnimationFrame(tick);
    }

    function onPointerMove(event) {
        if (event.pointerType === "touch" || !canAnimate()) return;

        lastPointerX = event.clientX;
        lastPointerY = event.clientY;
        lastInputAt = performance.now();
        updateTargetFromPoint(lastPointerX, lastPointerY);
        start();
    }

    function onResize() {
        readViewport();
        updateTargetFromPoint(lastPointerX, lastPointerY);
        start();
    }

    function bindMotionEvents() {
        window.addEventListener("pointermove", onPointerMove, {passive: true});
        window.addEventListener("resize", onResize, {passive: true});
        window.addEventListener("blur", settleToCenter);
        document.addEventListener("mouseleave", settleToCenter, {passive: true});
    }

    function unbindMotionEvents() {
        window.removeEventListener("pointermove", onPointerMove);
        window.removeEventListener("resize", onResize);
        window.removeEventListener("blur", settleToCenter);
        document.removeEventListener("mouseleave", settleToCenter);
    }

    function activate() {
        if (active || !canAnimate()) return;

        active = true;
        root.classList.add(ACTIVE_CLASS);
        bindMotionEvents();
        writeMotionVars(currentX, currentY);
    }

    function deactivate() {
        if (!active) return;

        active = false;
        stop();
        unbindMotionEvents();
        root.classList.remove(ACTIVE_CLASS);
        targetX = 0;
        targetY = 0;
        currentX = 0;
        currentY = 0;
        writeMotionVars(0, 0);
    }

    function syncState() {
        if (canAnimate()) {
            activate();
        } else {
            deactivate();
        }
    }

    function destroy() {
        deactivate();
        cleanupCallbacks.forEach((cleanup) => cleanup());
        cleanupCallbacks.length = 0;
    }

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
