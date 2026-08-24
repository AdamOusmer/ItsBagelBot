// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const QUOTES_MODULE: ModuleDef = 
{
  id: 'quotes',
  label: 'Quotes',
  tagline: 'Save the best things said on stream and replay them in chat.',
  description:
    'Keep a channel quote book. Someone saves a line with !addquote the text (or !quote "the text") and the bot numbers it and stamps the save date. Anyone replays one with !quote for a random pick, !quote 12 for a specific number, or !quote ferret for a random quote containing that word — the bot answers "Quote #12: the text (2026-07-11)". Choose who is allowed to save and who may rewrite one (!quote edit 12 the fixed text) below; both default to moderators. Mods remove a mistake with !quote remove 12; numbers are never reused, so an old number keeps pointing at the same quote. The replies are fixed system text (localized to your console language).',
  icon: 'quote',
  category: 'Community',
  defaultEnabled: false,
  // Bespoke page: the quote book (list + add/edit/remove) plus the enable
  // and save/edit permission settings live on /quotes, not the generic
  // reply page.
  href: '/quotes',
  // Like timers, the quote book also rides the 'commands' grant.
  delegateSections: ['modules', 'commands'],
  replies: [],
  commands: [
    { trigger: '!quote', summary: 'Post a random saved quote (also !quotes).' },
    { trigger: '!quote <number>', summary: 'Post that exact quote.' },
    { trigger: '!quote <word>', summary: 'Post a random quote containing that word.' },
    { trigger: '!addquote <text>', summary: 'Save a new quote (also !quoteadd or !quote "text"; who can save is set above); the save date is kept with it.' },
    { trigger: '!quote edit <number> <text>', summary: 'Rewrite a saved quote in place (who can edit is set above); its number and date stay.' },
    { trigger: '!quote remove <number>', summary: 'Delete a quote; its number is retired.', perm: 'mod' }
  ]
};
