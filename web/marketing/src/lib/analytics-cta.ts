// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { MarketingCTA } from '@bagel/kit/analytics-beacon-catalog';
import { usagePayload } from '@bagel/kit/analytics-beacon';
import { SITE } from '@bagel/kit/site-links';

const SECTIONS: [string, MarketingCTA][] = [
  ['.bb-nav, .bb-mobile-menu', 'nav'],
  ['.hero', 'hero'],
  ['.play', 'playground'],
  ['.finale', 'finale'],
  ['.tiers', 'pricing'],
  ['.bb-footer', 'footer'],
  ['.builder', 'builder']
];

const PAGES: [RegExp, MarketingCTA][] = [
  [/^\/(?:[a-z]{2}(?:-[a-z]{2})?\/)?vs(?:\/|$)/, 'compare'],
  [/^\/(?:[a-z]{2}(?:-[a-z]{2})?\/)?guides(?:\/|$)/, 'guide'],
  [/^\/(?:[a-z]{2}(?:-[a-z]{2})?\/)?import(?:\/|$)/, 'import']
];

function pagePlacement(pathname: string): MarketingCTA {
  return PAGES.find(([path]) => path.test(pathname))?.[1] ?? 'landing';
}

export function marketingPlacement(link: HTMLAnchorElement, pathname: string): MarketingCTA | undefined {
  const target = new URL(link.href);
  if (target.origin !== SITE.dashboard) return undefined;
  if (target.searchParams.get('install') === '1') return 'app';
  return SECTIONS.find(([selector]) => link.closest(selector))?.[1] ?? pagePlacement(pathname);
}

function onClick(event: MouseEvent): void {
  const link = event.target instanceof Element ? event.target.closest('a') : null;
  const dim = link ? marketingPlacement(link, location.pathname) : undefined;
  const payload = dim ? usagePayload({ event: 'marketing.cta', dim }) : null;
  if (payload) navigator.sendBeacon(`${SITE.dashboard}/b`, payload);
}

export function countCtaClicks(): void {
  document.addEventListener('click', onClick, { capture: true });
}
