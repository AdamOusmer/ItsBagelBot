// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The mobile nav panel's choreography: the panel slides down from behind the
// bar while the curve at its bottom edge flattens out and the items spring in
// one after another. Three concurrent tweens on different curves, which is the
// whole reason this is script and not a CSS transition -- the curve is an SVG
// path whose control point moves with progress, and each item's offset is a
// function of its index.
//
// Lifted from the marketing site's Nav.astro (a 230-line class in a component
// <script>), with two changes that are not refactors:
//
//  1. No `motion` dependency. The three `animate(0, 1, { ease: cubicBezier(…) })`
//     calls are now ./tween on the shared rAF scheduler. @bagel/ui declares one
//     runtime dependency and it is not an animation library.
//  2. No `document.body.classList.toggle('nav-menu-open')`. That class belonged
//     to the site's stylesheet, so the engine was writing a name it did not own
//     into a document it does not own -- an element in a library cannot assume
//     the page defines a rule for its class. The scroll lock is now
//     `body:has(.bb-mobile-menu.is-open)` in ui/styles/elements/nav.css, which
//     needs no JS at all and cannot fall out of sync with the panel state.
//
// The panel is measured, not assumed: `renderPath` reads the panel's box every
// frame because the curve spans its full width and the panel is a percentage
// of the viewport. That is one getBoundingClientRect per frame during a 950ms
// open, on an element nothing else is writing to, so it does not thrash layout.

import { bezier, tween } from './tween';
import { getSmoothScroll } from './lenis';

export interface TopDownMenuElements {
  /** The hamburger. Carries aria-expanded and the open class. */
  toggle: HTMLElement;
  /** The panel. Carries aria-hidden, inert, and the two state classes. */
  menu: HTMLElement;
  /** The <path> inside the panel's clipPath. */
  curvePath: SVGPathElement;
}

export interface TopDownMenuOptions {
  /** How far the bottom edge bows down while closed, in px. */
  curveDepth?: number;
  /** The closed panel's visible height, in px: the bar's own band. */
  initialHeight?: number;
  /** First item's closed offset, in px. Negative = above its resting spot. */
  initialOffset?: number;
  /** Added per item, so the last one starts furthest away. */
  offsetStep?: number;
  /** Above this width the panel does not exist and open() is a no-op. */
  breakpoint?: string;
  /** Swapped onto the toggle's aria-label. Caller's wording. */
  labels?: { open: string; close: string };
}

const DEFAULTS = {
  curveDepth: 220,
  initialHeight: 96,
  initialOffset: -34,
  offsetStep: -8,
  breakpoint: '(max-width: 1023px)',
} as const;

// The three curves, named for what they carry. The item curve overshoots
// (y2 > 1) on purpose: it is the only spring in the site's chrome, and it is
// what makes the panel feel dropped rather than slid.
const PANEL_EASE = bezier(0.33, 1, 0.68, 1);
const CURVE_EASE = bezier(0.25, 0.1, 0.25, 1);
const ITEM_EASE = bezier(0.34, 1.56, 0.64, 1);

const ITEM_SELECTOR = '[data-menu-item], [data-menu-footer]';

/** Panel travel: fully off the top of its band at 0, in place at 1. */
function renderMenu(menu: HTMLElement, progress: number): void {
  menu.style.transform = `translateY(${(1 - progress) * -100}%)`;
}

/**
 * The clip path: a rectangle whose bottom edge is a quadratic curve. At 0 it
 * is the bar's band with a deep bow; at 1 it is the full panel, flat.
 */
function renderPath(
  menu: HTMLElement,
  path: SVGPathElement,
  progress: number,
  options: Required<Omit<TopDownMenuOptions, 'labels'>>,
): void {
  const { width, height } = menu.getBoundingClientRect();
  // A panel measured while `display: none` reports 0x0, which would collapse
  // the clip path to nothing and hide a panel that is meant to be opening.
  // The viewport is the right answer in that case: it is what the panel fills.
  const w = Math.max(1, width || window.innerWidth);
  const h = Math.max(1, height || window.innerHeight);
  const currentHeight = options.initialHeight + (h - options.initialHeight) * progress;
  const curve = options.curveDepth * (1 - progress);

  path.setAttribute(
    'd',
    `M 0 0 L ${w} 0 L ${w} ${currentHeight} Q ${w / 2} ${currentHeight + curve}, 0 ${currentHeight} L 0 0 Z`,
  );
}

function renderItem(
  item: HTMLElement,
  index: number,
  progress: number,
  options: Required<Omit<TopDownMenuOptions, 'labels'>>,
): void {
  const offset = (options.initialOffset + options.offsetStep * index) * (1 - progress);
  item.style.opacity = String(progress);
  item.style.transform = `translateY(${offset}px)`;
}

/**
 * Mount the panel animator. Returns `dispose()`, which stops any tween in
 * flight and drops every listener; it does NOT reset the panel, because the
 * only caller that disposes is a page teardown.
 */
export function mountTopDownMenu(
  elements: TopDownMenuElements,
  options: TopDownMenuOptions = {},
): () => void {
  const { toggle, menu, curvePath } = elements;
  const config = { ...DEFAULTS, ...options };
  const compact = window.matchMedia(config.breakpoint);
  const items = Array.from(menu.querySelectorAll<HTMLElement>(ITEM_SELECTOR));

  let stops: (() => void)[] = [];
  let open = false;

  const stopAll = () => {
    for (const stop of stops) stop();
    stops = [];
  };

  const paint = (progress: number) => {
    renderMenu(menu, progress);
    renderPath(menu, curvePath, progress, config);
    items.forEach((item, i) => renderItem(item, i, progress, config));
  };

  const setA11y = (next: boolean) => {
    toggle.classList.toggle('is-open', next);
    toggle.setAttribute('aria-expanded', String(next));
    if (options.labels) {
      toggle.setAttribute('aria-label', next ? options.labels.close : options.labels.open);
    }
    menu.setAttribute('aria-hidden', String(!next));
    menu.toggleAttribute('inert', !next);
  };

  // Opening and closing are the same three tweens run in opposite directions,
  // so the panel can be reversed mid-flight without a special case.
  const animate = (next: boolean) => {
    const from = next ? 0 : 1;
    const to = next ? 1 : 0;

    stops.push(
      tween({
        duration: 0.7,
        ease: PANEL_EASE,
        from,
        to,
        onUpdate: (v) => renderMenu(menu, v),
        onComplete: () => {
          renderMenu(menu, to);
          if (!next) menu.classList.remove('is-visible');
        },
      }),
      tween({
        duration: 0.95,
        ease: CURVE_EASE,
        from,
        to,
        onUpdate: (v) => renderPath(menu, curvePath, v, config),
        onComplete: () => renderPath(menu, curvePath, to, config),
      }),
    );

    // Opening staggers forwards from the top and closing collapses upwards
    // twice as fast: a menu that takes as long to leave as it took to arrive
    // reads as unresponsive on the tap that dismissed it.
    items.forEach((item, index) => {
      const delay = next ? 0.1 + index * 0.07 : (items.length - 1 - index) * 0.035;
      stops.push(
        tween({
          duration: 0.72,
          delay,
          ease: ITEM_EASE,
          from,
          to,
          onUpdate: (v) => renderItem(item, index, v, config),
          onComplete: () => renderItem(item, index, to, config),
        }),
      );
    });
  };

  const setOpen = (next: boolean, animated = true) => {
    // Above the breakpoint there is no panel to open: the links are in the bar.
    const target = next && compact.matches;
    if (target === open && animated) return;

    stopAll();
    open = target;
    setA11y(target);
    menu.classList.toggle('is-open', target);
    if (target) menu.classList.add('is-visible');

    if (!animated) {
      paint(target ? 1 : 0);
      if (!target) menu.classList.remove('is-visible');
      return;
    }
    animate(target);
  };

  const onToggle = () => setOpen(!open);
  const onLink = () => setOpen(false);
  const onScrim = (event: MouseEvent) => {
    if (event.target === menu) setOpen(false);
  };
  const onKey = (event: KeyboardEvent) => {
    if (event.key === 'Escape' && open) setOpen(false);
  };
  // Crossing back to the desktop layout with the panel open would leave the
  // page scroll-locked behind a panel that is no longer displayed.
  const onViewport = (event: MediaQueryListEvent) => {
    if (!event.matches) setOpen(false, false);
  };
  const onResize = () => renderPath(menu, curvePath, open ? 1 : 0, config);
  // bfcache: a page restored with the panel open restores its DOM state too.
  const onPageHide = () => setOpen(false, false);

  const links = Array.from(menu.querySelectorAll('a'));

  paint(0);
  setA11y(false);

  toggle.addEventListener('click', onToggle);
  for (const link of links) link.addEventListener('click', onLink);
  menu.addEventListener('click', onScrim);
  document.addEventListener('keydown', onKey);
  compact.addEventListener('change', onViewport);
  window.addEventListener('resize', onResize, { passive: true });
  window.addEventListener('pagehide', onPageHide);

  return () => {
    stopAll();
    toggle.removeEventListener('click', onToggle);
    for (const link of links) link.removeEventListener('click', onLink);
    menu.removeEventListener('click', onScrim);
    document.removeEventListener('keydown', onKey);
    compact.removeEventListener('change', onViewport);
    window.removeEventListener('resize', onResize);
    window.removeEventListener('pagehide', onPageHide);
  };
}

/**
 * The brand mark's second job: clicking it while already on the page it links
 * to scrolls to the top instead of reloading the same document.
 *
 * Reads the smooth scroller through ./lenis's accessor rather than a global.
 * The version this replaces read `window.lenis` while the console's shell read
 * `window.__lenis`, and the two spellings were the same object under two names
 * -- so exactly one of the two surfaces got a smooth scroll, and which one
 * depended on which script had initialised it.
 */
export function mountHomeLogo(logo: HTMLAnchorElement): () => void {
  const onClick = (event: MouseEvent) => {
    const target = new URL(logo.href, window.location.href);
    const samePage =
      target.origin === window.location.origin &&
      target.pathname === window.location.pathname;
    if (!samePage) return;

    event.preventDefault();
    window.history.replaceState(window.history.state, '', target.pathname || '/');

    const scroller = getSmoothScroll();
    if (scroller) scroller.scrollTo(0, { duration: 0.9 });
    else window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  logo.addEventListener('click', onClick);
  return () => logo.removeEventListener('click', onClick);
}
