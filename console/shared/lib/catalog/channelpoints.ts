// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const CHANNELPOINTS_MODULE: ModuleDef = 
{
  id: 'channelpoints',
  label: 'Channel Points',
  tagline: 'Turn channel-point redemptions into bot actions.',
  description:
    'Create the custom rewards viewers redeem with channel points (made under the bot on Twitch, styled natively) and bind each one to a bot action, like posting a chat line. Choose whether each redemption is fulfilled, refunded, or left for a mod. Manage the rewards on this page.',
  icon: 'hex',
  category: 'Channel',
  defaultEnabled: false,
  href: '/channelpoints',
  // Channel Points is its own delegation grant (see SECTIONS in the settings
  // page), not part of the blanket 'modules' one.
  delegateSections: ['channelpoints'],
  replies: []
};
