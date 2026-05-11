/**
 * Page-level parallax. Writes CSS custom properties on the document root that
 * the global stylesheet consumes via `translate` and `transform` on top-level
 * page regions (header/main/section/footer) and on the ornaments frame.
 *
 * The motion is intentionally more pronounced than a subtle micro-interaction
 * — the goal is to make the page feel slightly weightless, drifting against
 * the cursor with a soft 3D tilt.
 */

const PAGE_OFFSET_X = 4;
const PAGE_OFFSET_Y = 3;
const PAGE_TILT_X = 0.25;
const PAGE_TILT_Y = 0.35;
const FRAME_RATIO = 0.4;

export class Parallax {
    constructor(pointer, root) {
        this.pointer = pointer;
        this.root = root;
        this.lastValues = new Map();
    }

    apply() {
        const x = this.pointer.smoothNX * -PAGE_OFFSET_X;
        const y = this.pointer.smoothNY * -PAGE_OFFSET_Y;
        const tiltX = this.pointer.smoothNY * -PAGE_TILT_X;
        const tiltY = this.pointer.smoothNX * PAGE_TILT_Y;

        this._setVar("--dom-motion-page-x", x, "px");
        this._setVar("--dom-motion-page-y", y, "px");
        this._setVar("--dom-motion-page-tilt-x", tiltX, "deg", 3);
        this._setVar("--dom-motion-page-tilt-y", tiltY, "deg", 3);
        this._setVar("--dom-motion-frame-x", x * FRAME_RATIO, "px");
        this._setVar("--dom-motion-frame-y", y * FRAME_RATIO, "px");
    }

    reset() {
        this._setVar("--dom-motion-page-x", 0, "px");
        this._setVar("--dom-motion-page-y", 0, "px");
        this._setVar("--dom-motion-page-tilt-x", 0, "deg", 3);
        this._setVar("--dom-motion-page-tilt-y", 0, "deg", 3);
        this._setVar("--dom-motion-frame-x", 0, "px");
        this._setVar("--dom-motion-frame-y", 0, "px");
    }

    _setVar(name, value, unit, precision = 2) {
        const next = `${value.toFixed(precision)}${unit}`;
        if (this.lastValues.get(name) === next) return;
        this.root.style.setProperty(name, next);
        this.lastValues.set(name, next);
    }
}
