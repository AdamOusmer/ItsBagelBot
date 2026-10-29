// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface GlideOptions {
  observe?: HTMLElement | null;
  selector?: string;
}

const DEFAULT_SELECTOR = '[data-active]';

// The container must be the offset parent of the selected items.
export function mountGlide(
  container: HTMLElement,
  options: GlideOptions = {},
): (() => void) & { measure: () => void } {
  const selector = options.selector ?? DEFAULT_SELECTOR;
  const glide = container.querySelector<HTMLElement>(':scope > .bb-glide');

  const measure = () => {
    if (!glide) return;
    const active = container.querySelector<HTMLElement>(selector);
    if (!active) {
      glide.removeAttribute('data-shown');
      return;
    }
    glide.setAttribute('data-shown', '');
    glide.style.setProperty('--glide-top', `${active.offsetTop}px`);
    glide.style.setProperty('--glide-left', `${active.offsetLeft}px`);
    glide.style.setProperty('--glide-w', `${active.offsetWidth}px`);
    glide.style.setProperty('--glide-h', `${active.offsetHeight}px`);
  };

  if (glide) {
    glide.style.transition = 'none';
    measure();
    void glide.offsetWidth;
    glide.style.transition = '';
  }

  const observer =
    options.observe && typeof ResizeObserver !== 'undefined'
      ? new ResizeObserver(measure)
      : null;
  observer?.observe(options.observe as HTMLElement);

  const dispose = () => observer?.disconnect();
  return Object.assign(dispose, { measure });
}
