// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { replyTokens, type ModuleDef } from './module-def';

export const CHANNELPOINTS_MODULE: ModuleDef =
{
  id: 'channelpoints',
  label: 'Channel Points',
  tagline: 'Turn channel-point redemptions into bot actions.',
  description:
    'Create the custom rewards viewers redeem with channel points (made under the bot on Twitch, styled natively) and bind each one to a bot action, like posting a chat line. Choose whether each redemption is fulfilled, refunded, or left for a mod. Manage the rewards on this page.',
  category: 'Channel',
  defaultEnabled: false,
  href: '/channelpoints',
  delegateSections: ['channelpoints'],
  replies: [
    {
      key: 'reply',
      label: 'Reward chat reply',
      tagline: 'What the bot posts when a bound reward is redeemed.',
      event: 'on redemption',
      messageKey: 'message',
      defaultMessage: '{user} redeemed {reward}!',
      tokens: replyTokens(
        ['user', 'input', 'reward', 'cost', 'channel', 'counter', 'points'],
        {
          user: 'sesame_sam',
          input: 'hello chat',
          reward: 'Say Hi',
          cost: '500',
          channel: 'streamer',
          counter: '12',
          points: '50'
        },
        'channelpoints.reply'
      )
    }
  ]
};
