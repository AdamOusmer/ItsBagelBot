/**
 * Pointer tracker. Owns the raw input listeners and exposes the cursor
 * position as `nx`/`ny` normalized to [-1, 1] from the viewport center.
 *
 * Each frame the orchestrator calls `step(ease)` to lerp the smoothed
 * `smoothNX`/`smoothNY` values toward the raw ones. Subsystems read the
 * smoothed values so their motion shares the same eased trajectory.
 */

function clamp(value, min, max) {
    return Math.min(max, Math.max(min, value));
}

export class Pointer {
    constructor() {
        this.viewportWidth = Math.max(1, window.innerWidth);
        this.viewportHeight = Math.max(1, window.innerHeight);

        this.nx = 0;
        this.ny = 0;
        this.smoothNX = 0;
        this.smoothNY = 0;

        this.lastMoveAt = 0;
        this.onActivity = null;

        this._onMove = this._onMove.bind(this);
        this._onResize = this._onResize.bind(this);
        this._onSettle = this._onSettle.bind(this);
    }

    _onMove(event) {
        if (event.pointerType === "touch") return;

        this.nx = clamp((event.clientX / this.viewportWidth - 0.5) * 2, -1, 1);
        this.ny = clamp((event.clientY / this.viewportHeight - 0.5) * 2, -1, 1);
        this.lastMoveAt = performance.now();

        if (this.onActivity) this.onActivity();
    }

    _onResize() {
        this.viewportWidth = Math.max(1, window.innerWidth);
        this.viewportHeight = Math.max(1, window.innerHeight);
        if (this.onActivity) this.onActivity();
    }

    _onSettle() {
        this.nx = 0;
        this.ny = 0;
        this.lastMoveAt = performance.now();
        if (this.onActivity) this.onActivity();
    }

    step(ease) {
        this.smoothNX += (this.nx - this.smoothNX) * ease;
        this.smoothNY += (this.ny - this.smoothNY) * ease;
    }

    isSettled(epsilon = 0.005) {
        return (
            Math.abs(this.nx - this.smoothNX) < epsilon &&
            Math.abs(this.ny - this.smoothNY) < epsilon
        );
    }

    bind() {
        window.addEventListener("pointermove", this._onMove, {passive: true});
        window.addEventListener("resize", this._onResize, {passive: true});
        window.addEventListener("blur", this._onSettle);
        document.addEventListener("mouseleave", this._onSettle, {passive: true});
    }

    unbind() {
        window.removeEventListener("pointermove", this._onMove);
        window.removeEventListener("resize", this._onResize);
        window.removeEventListener("blur", this._onSettle);
        document.removeEventListener("mouseleave", this._onSettle);
    }
}
