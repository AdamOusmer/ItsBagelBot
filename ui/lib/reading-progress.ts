// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Reading progress: writes `--read` (0..1) onto a fill element as the document
 * scrolls, for the 2px bar at the top of the viewport
 * (ui/styles/elements/reading-progress.css).
 *
 * IT MEASURES THE DOCUMENT, not a container, and that is the bug it was
 * written to fix. The per-shell bars this replaced each measured their own
 * `<article>`: on a page whose article ended above the footer the bar hit 100%
 * with a screen of content left, and on the guides shell (an article inside a
 * sticky-sidebar grid) `scrollHeight` was the grid's, so it never reached 100%
 * at all. `documentElement.scrollHeight - innerHeight` is the only quantity
 * that means "how much of this page is left".
 *
 * ONE PASSIVE SCROLL LISTENER, coalesced onto the shared frame loop
 * (ui/lib/raf-loop). Passive because this handler never calls
 * `preventDefault`, and declaring that lets the browser keep scrolling without
 * waiting to find out. Coalesced because scroll fires far more often than the
 * compositor paints: on a trackpad flick the raw event rate is several times
 * the frame rate, and writing the custom property on each one is that many
 * style recalculations for one visible change.
 *
 * `resize` is listened to for the same reason as `scroll` and is not an
 * afterthought: the denominator is `innerHeight`, so a rotation, a split view,
 * or the iOS URL bar collapsing changes the answer without any scrolling
 * happening at all.
 *
 * FOUR DECIMAL PLACES on the written value. `scaleX` at 2px tall cannot show
 * more, and the string is what the style system diffs — trimming it is what
 * makes an idle-but-jittering scroll position stop invalidating style.
 */

import { subscribe } from './raf-loop';

/**
 * Drive `fill` from the document's scroll position. Returns the dispose;
 * call it before the element is detached.
 */
export function mountReadingProgress(fill: HTMLElement): () => void {
    if (typeof window === 'undefined') return () => {};

    let queued = false;
    let unsubscribe: (() => void) | null = null;

    const update = (): boolean => {
        queued = false;
        unsubscribe = null;
        const doc = document.documentElement;
        const total = doc.scrollHeight - window.innerHeight;
        const read = total > 0 ? Math.min(1, Math.max(0, window.scrollY / total)) : 0;
        fill.style.setProperty('--read', read.toFixed(4));
        // One frame per burst of events: the tick settles immediately and the
        // next scroll event re-subscribes.
        return false;
    };

    const onScroll = (): void => {
        if (queued) return;
        queued = true;
        unsubscribe = subscribe(update);
    };

    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onScroll, { passive: true });
    update();

    return () => {
        window.removeEventListener('scroll', onScroll);
        window.removeEventListener('resize', onScroll);
        unsubscribe?.();
        queued = false;
    };
}
