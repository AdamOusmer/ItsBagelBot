// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface GlideOptions {
  observe?: HTMLElement | null;
  selector?: string;
  // Follow selection changes made outside the caller (scroll-spy, a sibling
  // script toggling classes) by re-measuring when the items' state flips.
  watch?: boolean;
}

const WATCHED = ['class', 'aria-checked', 'aria-current', 'data-active'];

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

  const watcher =
    options.watch && typeof MutationObserver !== 'undefined' ? new MutationObserver(measure) : null;
  watcher?.observe(container, { subtree: true, childList: true, attributes: true, attributeFilter: WATCHED });

  const dispose = () => {
    observer?.disconnect();
    watcher?.disconnect();
  };
  return Object.assign(dispose, { measure });
}

// Astro pages have no component lifecycle: bind every tab strip that carries a
// glide once, and again after each client-side navigation.
export function mountTabGlides(root: ParentNode = document): void {
  root.querySelectorAll<HTMLElement>('.bb-tabs:not([data-bb-glide])').forEach((tabs) => {
    if (!tabs.querySelector(':scope > .bb-glide')) return;
    tabs.dataset.bbGlide = '';
    mountGlide(tabs, { observe: tabs, selector: ':scope > .bb-tab.is-active', watch: true });
  });
}
