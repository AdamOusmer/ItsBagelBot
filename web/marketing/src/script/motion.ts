// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The site's reduced-motion query, now owned by the design library.
 *
 * This file used to hold the MediaQueryList itself. It moved to
 * `@bagel/ui/lib/motion-query`, where `interactions.js`'s fine-pointer query,
 * `dom-motion`'s pair and the console's two inline `matchMedia` calls joined
 * it, so that four copies of those two media-feature strings became one. A
 * mistyped media feature is not an error — it is a query that never matches,
 * forever — which is why they are worth naming once.
 *
 * It stays behind as a re-export rather than being deleted because a dozen
 * component scripts import `../../script/motion`. The cursor has since moved
 * to `@bagel/ui/astro/Cursor.astro` and no longer goes through here, so the
 * one importer that was worth waiting for is gone; what is left is a flat
 * rename across files that other in-flight PRs are editing, and doing it from
 * here would collide with them. It is the last thing to delete once the
 * element PRs have landed.
 *
 * Nothing new may be added here. New motion state belongs in the library.
 */

export { finePointer, hasFinePointer, prefersReducedMotion, reduceMotion } from '@bagel/ui/lib/motion-query';
export type { MotionQuery } from '@bagel/ui/lib/motion-query';
