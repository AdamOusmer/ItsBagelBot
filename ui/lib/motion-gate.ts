// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Marks every `[data-motion]` region `on` while it is near the viewport and
 * `off` otherwise; styles/motion.css drops the CSS loops inside an `off`
 * region, so they give their layers back and restart from their first frame
 * when the region returns. Loops kept running off screen still cost WebKit
 * memory: measured 2026-09-23 at 390x844, device scale 3, the marketing home
 * page held 30-65 MB more with every section's loops running than with only
 * the visible section's.
 *
 * Only for regions whose animations are loops. A one-shot entrance with
 * fill-mode forwards inside a gated region would replay on every return.
 */
export function observeMotion(root: ParentNode = document): () => void {
    const regions = root.querySelectorAll<HTMLElement>('[data-motion]');
    if (!regions.length || typeof IntersectionObserver === 'undefined') return () => {};

    const observer = new IntersectionObserver((entries) => {
        for (const entry of entries) {
            (entry.target as HTMLElement).dataset.motion = entry.isIntersecting ? 'on' : 'off';
        }
    }, { rootMargin: '150px' });

    regions.forEach((region) => observer.observe(region));
    return () => observer.disconnect();
}
