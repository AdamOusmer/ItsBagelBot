// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface GlideOptions {
  navEl?: HTMLElement | null;
  selector?: string;
}

const DEFAULT_SELECTOR = '.bb-rail-item[data-active]';

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
