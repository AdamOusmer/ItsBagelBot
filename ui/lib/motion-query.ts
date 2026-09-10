// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The two media queries every motion decision in this design system is made
 * from, created once each.
 *
 * Before this file the marketing site had `src/script/motion.ts` (one
 * reduced-motion MediaQueryList, thirteen scripts previously each calling
 * `matchMedia` themselves), `interactions.js` had its own fine-pointer query on
 * line 18, `dom-motion/index.js` had a third pair, and the console re-typed the
 * reduced-motion string inline in `kit/lib/actions.ts` twice. Four copies of two
 * strings, and a mistyped media feature is not an error: `matchMedia('(prefers-
 * reduce-motion: reduce)')` parses, never matches, and silently animates for
 * everyone who asked it not to. Naming them once is the only way that typo can
 * be made once.
 *
 * They are module-level constants rather than the return value of a function
 * because a scene that reacts to the setting changing mid-session listens on
 * the object with `addEventListener('change', ...)`. Every listener has to be
 * on the SAME MediaQueryList or a change fires for some of them and not others,
 * which is exactly the bug shape that hides until someone flips the OS toggle
 * with the page open.
 */

/**
 * The slice of MediaQueryList this library uses.
 *
 * Deliberately not `MediaQueryList` itself: these constants are evaluated at
 * module load, and this module is imported (transitively) by Svelte components
 * that render on the server, where `window` does not exist. A narrow structural
 * type lets the no-window branch below return a real object instead of a lie
 * cast from `{}` — a real MediaQueryList has `media`, `onchange`, `dispatchEvent`
 * and the deprecated `addListener` pair, none of which anything here calls, and
 * faking all of them to satisfy the DOM type would be more code that means less.
 */
export type MotionQuery = {
    readonly matches: boolean;
    addEventListener(type: 'change', listener: () => void): void;
    removeEventListener(type: 'change', listener: () => void): void;
};

/**
 * Server-side stand-in. `matches: false` on both queries is the right default
 * and not an accident of it being the falsy one: no motion runs during SSR, so
 * "the visitor has not asked for reduced motion" and "there is no fine pointer"
 * are both answers to questions nobody is acting on yet. The client re-evaluates
 * on hydration, when the real queries exist.
 */
const ABSENT: MotionQuery = {
    matches: false,
    addEventListener() {},
    removeEventListener() {},
};

function query(feature: string): MotionQuery {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return ABSENT;
    return window.matchMedia(feature);
}

/** True while the visitor has asked the OS for reduced motion. */
export const reduceMotion: MotionQuery = query('(prefers-reduced-motion: reduce)');

/**
 * A pointer that can hover and land precisely — a mouse or a trackpad.
 *
 * `(hover: hover)` and `(pointer: fine)` are both required, and the pair is not
 * redundant: a stylus is fine but cannot hover, and some Android browsers report
 * `hover: hover` on a touchscreen. Effects gated on this one (cursor followers,
 * magnetic buttons, pointer parallax) are meaningless without a resting pointer
 * position, so both halves have to hold.
 */
export const finePointer: MotionQuery = query('(hover: hover) and (pointer: fine)');

/** True when the visitor has asked for reduced motion right now. */
export function prefersReducedMotion(): boolean {
    return reduceMotion.matches;
}

/** True when a hovering, precise pointer is driving the page right now. */
export function hasFinePointer(): boolean {
    return finePointer.matches;
}
