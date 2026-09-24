// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

export type GoveeOnRedeem = 'fulfill' | 'cancel' | 'leave';

export interface GoveeDevice {
  device: string;
  sku: string;
  name: string;
  color: boolean;
}

export interface GoveeReward {
  rewardId: string;
  title: string;
  cost: number;
  color: string;
  cooldown: number;
}

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
