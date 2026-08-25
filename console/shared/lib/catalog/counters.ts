// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const COUNTERS_MODULE: ModuleDef = 
{
  id: 'counters',
  label: 'Counters',
  tagline: 'Track wins, deaths, hugs, redeems — anything your chat can count.',
  description:
    'Create channel-wide, per-command, or per-viewer tallies. Adjust them from the dashboard or chat, and bump them from command responses and channel-point rewards.',
  icon: 'tally',
  category: 'Points',
  defaultEnabled: true,
  toggleable: false,
  href: '/counters',
  // The default Modules delegation scope gives delegates access to the full
  // counter book; commands-only delegates retain picker access separately.
  replies: []
};
