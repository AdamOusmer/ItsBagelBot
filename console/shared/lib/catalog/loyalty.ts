// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const LOYALTY_MODULE: ModuleDef = 
{
  id: 'loyalty',
  label: 'Loyalty Points',
  tagline: 'Viewers earn channel currency for subs, cheers and watch time.',
  description:
    'Give your community its own currency: viewers earn points for subscribing, resubscribing, gifting subs, cheering bits, and simply watching (everyone in chat earns on a 5-minute tick while you are live). Name the currency, tune every rate, and let mods grant points with !points set/add. Viewers check their standing with !points. Pairs with Counters and channel-point rewards that award points.',
  icon: 'coin',
  category: 'Community',
  defaultEnabled: false,
  href: '/loyalty',
  replies: []
};
