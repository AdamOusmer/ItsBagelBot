// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const ALERTS_MODULE: ModuleDef = 
{
  id: 'alerts',
  label: 'Chat Alerts',
  tagline: 'Announce follows, subs, cheers, raids and ad breaks in chat.',
  description:
    'The bot posts a chat line when someone follows, subscribes, cheers, or raids, and can announce ad breaks. Turn each alert on or off and customize its message. New alerts default on, except the ad-break alert which stays off until you enable it. A follow alert fires at most once per viewer every three days, so unfollowing and refollowing cannot spam your chat.',
  icon: 'bell',
  category: 'Community',
  defaultEnabled: true,
  replies: [
    {
      key: 'follow',
      label: 'Follow alert',
      tagline: 'When someone follows your channel. Fires once per viewer every three days.',
      event: 'on follow',
      enableKey: 'followEnabled',
      messageKey: 'followMessage',
      defaultMessage: 'Thank you for following the channel, {user}!',
      // Per-alert tokens/samples mirror the maps in app/sesame/modules/alerts.go.
      tokens: ['user'],
      previewSamples: { user: 'sesame_sam' }
    },
    {
      key: 'sub',
      label: 'Subscribe alert',
      tagline: 'When someone subscribes or resubscribes.',
      event: 'on subscribe',
      enableKey: 'subEnabled',
      messageKey: 'subMessage',
      defaultMessage: 'Welcome to the community, {user}! Thank you for subscribing!',
      tokens: ['user', 'tier'],
      previewSamples: { user: 'sesame_sam', tier: '1000' }
    },
    {
      key: 'gift',
      label: 'Gift sub alert',
      tagline: 'When someone gifts subs. One line per gifter, never per recipient.',
      event: 'on gift subs',
      enableKey: 'giftEnabled',
      messageKey: 'giftMessage',
      defaultMessage: '{user} just gifted {count} subs to the community! Thank you!',
      tokens: ['user', 'count', 'tier'],
      previewSamples: { user: 'GenerousViewer', count: '5', tier: '1000' }
    },
    {
      key: 'cheer',
      label: 'Cheer alert',
      tagline: 'When someone cheers bits.',
      event: 'on cheer',
      enableKey: 'cheerEnabled',
      messageKey: 'cheerMessage',
      defaultMessage: 'Thank you for the {bits} bits, {user}!',
      tokens: ['user', 'bits'],
      previewSamples: { user: 'sesame_sam', bits: '500' }
    },
    {
      key: 'raid',
      label: 'Raid alert',
      tagline: 'When another channel raids in.',
      event: 'on raid',
      enableKey: 'raidEnabled',
      messageKey: 'raidMessage',
      defaultMessage: '{user} is raiding the channel with {viewers} viewers! Welcome everyone!',
      tokens: ['user', 'viewers'],
      previewSamples: { user: 'CrustyCrumbs', viewers: '42' }
    },
    {
      key: 'ads',
      label: 'Ad break alert',
      tagline: 'When an ad break starts. Off by default.',
      event: 'on ad break',
      enableKey: 'adsEnabled',
      messageKey: 'adsMessage',
      defaultOff: true,
      defaultMessage: "Ads are rolling for {duration} seconds. Hang tight, we'll be right back!",
      tokens: ['duration'],
      previewSamples: { duration: '90' }
    }
  ]
};
