// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const AUTOMATIONS_MODULE: ModuleDef =
{
  id: 'automations',
  beta: true,
  label: 'Automations',
  tagline: 'Chain a trigger, conditions and actions the bot runs for you.',
  description:
    'Build your own chains: pick a trigger, add conditions, and line up actions the bot runs for you.',
  category: 'Chat',
  defaultEnabled: false,
  href: '/automations',
  delegateSections: ['modules'],
  replies: []
};
