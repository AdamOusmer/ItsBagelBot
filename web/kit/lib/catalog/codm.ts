// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';
import { LINKED_ONLY_FIELD } from './shared-fields';

export const CODM_MODULE: ModuleDef = {
  id: 'codm',
  label: 'CODM Profile',
  tagline: 'Call of Duty: Mobile Global profile lookups in chat.',
  description:
    'Look up a CODM Global profile by UID or exact nickname with !codm, !codmprofile or !codmrank. The read-only profile includes level, Multiplayer rank and rating, country and short ID. Supports the CODM Global edition and does not include KD, match history or Battle Royale stats.',
  category: 'Stats',
  defaultEnabled: false,
  replies: [
    {
      key: 'profile',
      label: '!codm',
      tagline: 'CODM Global profile: !codm, !codmprofile or !codmrank.',
      event: '!codm [UID/exact nickname]',
      command: 'codm',
      previewArgs: 'iFerg',
      enableKey: 'profileEnabled',
      messageKey: 'profileMessage',
      defaultMessage: '{player} · level {level} · MP {rank} · {rating} rating · {country}',
      tokens: ['player', 'level', 'rank', 'rankclass', 'rating', 'country', 'shortid'],
      previewSamples: {
        player: 'iFerg',
        level: '414',
        rank: 'Master I',
        rankclass: '21',
        rating: '4590',
        country: 'US',
        shortid: 'IFERG'
      }
    }
  ],
  settings: [
    {
      key: 'account',
      label: 'Linked CODM account',
      type: 'text',
      placeholder: 'CODM UID or exact nickname',
      help: 'Default profile for the command. If blank, enter a CODM UID or exact nickname after !codm.'
    },
    {
      ...LINKED_ONLY_FIELD,
      help: 'When on, every lookup uses your linked CODM account and ignores viewer-supplied names. Link a CODM UID or exact nickname first.'
    }
  ]
};
