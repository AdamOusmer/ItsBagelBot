// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const GOVEE_MODULE: ModuleDef = 
{
  id: 'govee',
  label: 'Govee Lights',
  tagline: 'Let viewers recolour your Govee lights with channel points.',
  description:
    'Viewers redeem a channel-points reward, type a colour (a name like "blue" or a hex like #00ccff), and the bot turns your Govee light on and sets it. Live only: redemptions off-stream are refunded automatically. To get your Govee API key: open the Govee Home app, tap Profile (bottom right), tap the settings gear (top right), tap "Apply for API Key", fill in the short form, and Govee emails you a key within a few minutes. Then on this page: paste the key (we store it encrypted and never show it back), pick the light to control, and create the reward.',
  icon: 'power',
  category: 'Community',
  defaultEnabled: false,
  // The generic reply page cannot express key custody + a device picker, so
  // the tile opens a bespoke inspector instead.
  href: '/govee',
  replies: []
};
