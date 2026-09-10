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
 * It stays behind as a re-export rather than being deleted because twelve
 * component scripts import `../../script/motion`, and one of them is
 * `components/ui/Cursor.astro`, which the next PR rewrites onto the shared
 * cursor engine. Rewriting twelve import paths now and the cursor again next
 * week is churn spread over two PRs for no behaviour change; when the cursor
 * lands, this file goes and those imports point at the package.
 *
 * Nothing new may be added here. New motion state belongs in the library.
 */

export { finePointer, hasFinePointer, prefersReducedMotion, reduceMotion } from '@bagel/ui/lib/motion-query';
export type { MotionQuery } from '@bagel/ui/lib/motion-query';
