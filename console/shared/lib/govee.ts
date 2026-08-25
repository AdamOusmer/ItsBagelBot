// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// GOVEE_COLOR_NAMES are the colour words a viewer may type in the Govee reward
// input. It mirrors the sesame colour parser's named palette
// (app/sesame/modules/color.go) so the dashboard prompt/help never advertises a
// name the bot would then refuse; viewers can always give a hex code instead.
export const GOVEE_COLOR_NAMES: readonly string[] = [
  'red',
  'orange',
  'yellow',
  'green',
  'lime',
  'teal',
  'cyan',
  'blue',
  'navy',
  'purple',
  'violet',
  'indigo',
  'pink',
  'magenta',
  'white',
  'warm',
  'gold'
];

// Govee module shapes, shared by the server store and the dashboard components.
// The module binds channel-points rewards to smart lights: a viewer redeems a
// reward, types a colour (or "off"), and the bot drives that reward's light.
// One reward per light. The Twitch reward is owned by outgress; the bindings
// live in the "govee" module blob and are read by sesame's govee module.
export type GoveeOnRedeem = 'fulfill' | 'cancel' | 'leave';

// GoveeDevice is one controllable light on the broadcaster's Govee account.
export interface GoveeDevice {
  device: string;
  sku: string;
  name: string;
  color: boolean;
}

// GoveeReward mirrors the Twitch reward settings the dashboard shows for a light.
export interface GoveeReward {
  rewardId: string;
  title: string;
  cost: number;
  color: string;
  cooldown: number;
}

// GoveeBinding ties one reward to one light plus the behaviour sesame reads.
export interface GoveeBinding {
  device: string;
  sku: string;
  deviceName: string;
  onRedeem: GoveeOnRedeem;
  rewardId: string;
  reward: GoveeReward | null;
  allowOffline: boolean;
  allowOff: boolean;
  replyMessage: string;
}
