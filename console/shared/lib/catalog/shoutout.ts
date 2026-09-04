// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const SHOUTOUT_MODULE: ModuleDef = 
{
  id: 'shoutout',
  label: 'Auto Shoutout',
  tagline: 'Welcome incoming raids with an automatic shoutout.',
  description:
    'When another channel raids in, the bot posts a shoutout pointing your chat at the raider. Turn the module on and customize the shoutout line.',
  icon: 'megaphone',
  category: 'Channel',
  defaultEnabled: false,
  replies: [
    {
      key: 'shoutout',
      label: 'Raid shoutout',
      tagline: 'Automated chat shoutout when raided',
      event: 'on raid',
      messageKey: 'message',
      defaultMessage:
        'Massive shoutout to {raider} for the raid with {viewers} viewers! Check them out at twitch.tv/{raider.login}',
      // Tokens the shoutout module resolves (app/twitch/sesame/modules/shoutout.go).
      tokens: ['raider', 'raider.login', 'viewers'],
      previewSamples: { raider: 'CrustyCrumbs', 'raider.login': 'crustycrumbs', viewers: '42' }
    }
  ],
  settings: [
    {
      key: 'native_shoutout',
      label: 'Also send Twitch shoutout',
      type: 'toggle',
      help: "Fires Twitch's own /shoutout on the raider alongside the chat line, which shows their current category and profile card natively. Off by default."
    }
  ]
};
