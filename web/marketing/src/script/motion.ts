// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The site's one reduced-motion query. Thirteen scripts each called
 * `window.matchMedia('(prefers-reduced-motion: reduce)')`, which is thirteen
 * chances to typo the feature name into a query that never matches and fails
 * silently (a mistyped media feature is simply `false`, forever).
 *
 * The MediaQueryList is created once at module load, not per call: a scene that
 * needs to react to the setting changing mid-session listens on this object, so
 * every listener has to be on the same one.
 */
export const reduceMotion: MediaQueryList = window.matchMedia('(prefers-reduced-motion: reduce)');

/** True when the visitor has asked for reduced motion right now. */
export function prefersReducedMotion(): boolean {
    return reduceMotion.matches;
}
