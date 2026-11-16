// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { MarketingCTA } from '@bagel/kit/analytics-beacon-catalog';
import { SITE } from '@bagel/kit/site-links';

const PLACEMENTS: [string, MarketingCTA][] = [
  ['[data-analytics-cta="app"]', 'app'],
  ['.bb-nav, .bb-mobile-menu', 'nav'],
  ['.hero', 'hero'],
  ['.play', 'playground'],
  ['.finale', 'finale'],
  ['.tiers', 'pricing'],
  ['.bb-footer', 'footer'],
  ['[data-analytics-cta="compare"]', 'compare'],
  ['[data-analytics-cta="guide"]', 'guide'],
  ['[data-analytics-cta="import"]', 'import'],
  ['.builder', 'builder']
];

export function marketingPlacement(link: HTMLAnchorElement): MarketingCTA | undefined {
  const target = new URL(link.href);
  if (target.origin !== SITE.dashboard) return undefined;
  return PLACEMENTS.find(([selector]) => link.closest(selector))?.[1] ?? 'landing';
}
