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

export type GoveeSceneValue = number | { paramId: number; id: number };

export interface GoveeScene {
  id: string;
  name: string;
  type: string;
  instance: string;
  value: GoveeSceneValue;
}

export interface GoveeReward {
  rewardId: string;
  title: string;
  cost: number;
  color: string;
  cooldown: number;
}

export interface GoveeRewardBinding {
  onRedeem: GoveeOnRedeem;
  rewardId: string;
  reward: GoveeReward | null;
  allowOffline: boolean;
  allowOff: boolean;
  replyMessage: string;
}

export interface GoveeBinding extends GoveeRewardBinding {
  device: string;
  sku: string;
  deviceName: string;
}

export interface GoveeLight {
  device: string;
  sku: string;
  name: string;
}

export interface GoveeGroup extends GoveeRewardBinding {
  name: string;
  lights: GoveeLight[];
}

export const GOVEE_GROUP_MIN_LIGHTS = 2;
export const GOVEE_GROUP_MAX_LIGHTS = 32;
export const GOVEE_GROUP_NAME_MAX = 60;
