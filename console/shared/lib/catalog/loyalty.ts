// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const LOYALTY_MODULE: ModuleDef = 
{
  id: 'loyalty',
  label: 'Loyalty Points',
  tagline: 'Viewers earn channel currency for subs, cheers and watch time.',
  description:
    'Give your community its own currency: viewers earn points for subscribing, resubscribing, gifting subs, cheering bits, and simply watching (everyone in chat earns on a 5-minute tick while you are live). Name the currency, tune every rate, and let mods grant points with !points set/add. Viewers check their standing with !points. Gamble and Duels spend this ledger and are armed from this page — they cannot run while the currency is off. Pairs with Counters and channel-point rewards that award points.',
  icon: 'coin',
  category: 'Points',
  defaultEnabled: false,
  href: '/loyalty',
  replies: [],
  commands: [
    { trigger: '!points', summary: 'Check your balance and watch time.' },
    { trigger: '!points give @user 500', summary: "Give some of your own points to another viewer." },
    { trigger: '!leaderboard', summary: "Show the channel's top standings." },
    { trigger: '!points set @user 500', summary: "Set a viewer's balance.", perm: 'mod' },
    { trigger: '!points add @user 500', summary: 'Grant points to a viewer.', perm: 'mod' },
    { trigger: '!points remove @user 500', summary: 'Remove points from a viewer.', perm: 'mod' },
    { trigger: '!counter', summary: 'Manage counters (also on the Counters page).', perm: 'mod' }
  ]
};
