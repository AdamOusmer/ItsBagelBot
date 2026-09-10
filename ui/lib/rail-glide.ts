// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The rail's single gliding highlight: one block that travels to whichever row
// is current, across group boundaries.
//
// Measured rather than drawn per row, and that is the design: a per-row
// `background` on the active item cannot animate BETWEEN two elements, so the
// highlight would blink from one row to the next. Reading offsetTop/offsetHeight
// off the active row and writing them as two custom properties lets one
// absolutely-positioned box transition its top and height instead.
//
// The ResizeObserver is not defensive scaffolding, it is the fix for a
// reproducible bug: a sub-list opens or closes over 200ms, so every row under
// it keeps moving well after the state change that started it. Measuring only
// on that change left the highlight at the row's pre-collapse offset -- open
// Modules, go to Settings, collapse Modules, and the highlight stayed where
// Settings used to be. Watching the nav box re-measures on each frame the
// collapse reflows, so the block tracks the row instead of a stale number.

export interface GlideOptions {
  /** The box whose reflows re-measure. The rail's nav column. */
  navEl?: HTMLElement | null;
  /** How the active row is found inside the rail. */
  selector?: string;
}

const DEFAULT_SELECTOR = '.bb-rail-item[data-active]';

/**
 * Mount the glide on `railEl`, which must be the positioned ancestor the
 * highlight is absolutely placed inside (offsetTop is relative to it).
 *
 * Returns `dispose()`. Call `measure()` on it after changing the nav by hand;
 * a caller that renders the rail from reactive state does not need to -- the
 * observer covers the reflow that any such change causes.
 */
export function mountGlide(
  railEl: HTMLElement,
  options: GlideOptions = {},
): (() => void) & { measure: () => void } {
  const selector = options.selector ?? DEFAULT_SELECTOR;
  const glide = railEl.querySelector<HTMLElement>('.bb-rail__glide');

  const measure = () => {
    if (!glide) return;
    const active = railEl.querySelector<HTMLElement>(selector);
    if (!active) {
      glide.removeAttribute('data-shown');
      return;
    }
    glide.setAttribute('data-shown', '');
    glide.style.setProperty('--glide-top', `${active.offsetTop}px`);
    glide.style.setProperty('--glide-h', `${active.offsetHeight}px`);
  };

  measure();

  const observer =
    options.navEl && typeof ResizeObserver !== 'undefined'
      ? new ResizeObserver(measure)
      : null;
  observer?.observe(options.navEl as HTMLElement);

  const dispose = () => observer?.disconnect();
  return Object.assign(dispose, { measure });
}
