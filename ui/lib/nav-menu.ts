// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { bezier, tween } from './tween';
import { getSmoothScroll } from './lenis';

export interface TopDownMenuElements {
  toggle: HTMLElement;
  menu: HTMLElement;
  curvePath: SVGPathElement;
}

export interface TopDownMenuOptions {
  curveDepth?: number;
  initialHeight?: number;
  initialOffset?: number;
  offsetStep?: number;
  breakpoint?: string;
  labels?: { open: string; close: string };
}

const DEFAULTS = {
  curveDepth: 220,
  initialHeight: 96,
  initialOffset: -34,
  offsetStep: -8,
  breakpoint: '(max-width: 1023px)',
} as const;

const PANEL_EASE = bezier(0.33, 1, 0.68, 1);
const CURVE_EASE = bezier(0.25, 0.1, 0.25, 1);
const ITEM_EASE = bezier(0.34, 1.56, 0.64, 1);

const ITEM_SELECTOR = '[data-menu-item], [data-menu-footer]';

function renderMenu(menu: HTMLElement, progress: number): void {
  menu.style.transform = `translateY(${(1 - progress) * -100}%)`;
}

function renderPath(
  menu: HTMLElement,
  path: SVGPathElement,
  progress: number,
  options: Required<Omit<TopDownMenuOptions, 'labels'>>,
): void {
  const { width, height } = menu.getBoundingClientRect();
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
  const onViewport = (event: MediaQueryListEvent) => {
    if (!event.matches) setOpen(false, false);
  };
  const onResize = () => renderPath(menu, curvePath, open ? 1 : 0, config);
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
