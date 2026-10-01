// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface NavFitSample {
  available: number;
  needed: number;
}

const DEFAULT_HYSTERESIS = 12;

export function decideNavCollapse(
  sample: NavFitSample,
  collapsed: boolean,
  hysteresis = DEFAULT_HYSTERESIS,
): boolean {
  if (!collapsed && sample.needed > sample.available) return true;
  if (collapsed && sample.needed + hysteresis <= sample.available) return false;
  return collapsed;
}

export interface NavFitElements {
  bar: HTMLElement;
  brand: HTMLElement;
  links: HTMLElement;
  actions: HTMLElement;
}

export function navFitElements(bar: HTMLElement): NavFitElements | null {
  const pick = (selector: string) => bar.querySelector<HTMLElement>(selector);
  const found = { brand: pick('.bb-nav__brand'), links: pick('.bb-nav__links'), actions: pick('.bb-nav__actions') };
  if (!pick('[data-menu-toggle]') || Object.values(found).includes(null)) return null;
  return { bar, ...found } as NavFitElements;
}

function px(style: CSSStyleDeclaration, property: string): number {
  return parseFloat(style.getPropertyValue(property)) || 0;
}

function measure({ bar, brand, links, actions }: NavFitElements): NavFitSample {
  const barStyle = getComputedStyle(bar);
  const innerStyle = getComputedStyle(brand.parentElement ?? bar);
  const available = bar.clientWidth - px(barStyle, 'padding-left') - px(barStyle, 'padding-right');
  const chrome =
    px(barStyle, '--nav-pad-start') +
    px(barStyle, '--nav-pad-end') +
    px(innerStyle, 'border-left-width') +
    px(innerStyle, 'border-right-width') +
    2 * px(barStyle, '--nav-gap');
  const content = [brand, links, actions].reduce((sum, el) => sum + el.getBoundingClientRect().width, 0);
  return { available, needed: chrome + content };
}

export function mountNavFit(elements: NavFitElements, hysteresis = DEFAULT_HYSTERESIS): () => void {
  const { bar, links, actions } = elements;
  let collapsed = bar.dataset.navCollapsed === 'true';

  const write = () => {
    bar.dataset.navCollapsed = String(collapsed);
    links.toggleAttribute('inert', collapsed);
    actions.toggleAttribute('inert', collapsed);
  };

  const apply = (next: boolean) => {
    if (next === collapsed) return;
    collapsed = next;
    write();
  };

  const check = () => apply(decideNavCollapse(measure(elements), collapsed, hysteresis));

  // `.bb-nav` spans the viewport and never resizes when only its text grows.
  const observer = new ResizeObserver(check);
  observer.observe(elements.brand);
  observer.observe(links);
  observer.observe(actions);

  window.addEventListener('resize', check, { passive: true });
  document.fonts?.ready.then(check).catch(() => {});
  write();
  check();

  return () => {
    observer.disconnect();
    window.removeEventListener('resize', check);
  };
}
