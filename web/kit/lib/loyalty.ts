// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface LoyaltyConfig {
  pointsName: string;
  subPoints: number;
  resubPoints: number;
  giftSubPoints: number;
  cheerPointsPer100: number;
  watchPointsPerTick: number;
  modSetPoints: number;
  modAdjustPoints: number;
  viewerTransfers: number;
}

export const LOYALTY_DEFAULTS: LoyaltyConfig = {
  pointsName: 'points',
  subPoints: 500,
  resubPoints: 500,
  giftSubPoints: 100,
  cheerPointsPer100: 50,
  watchPointsPerTick: 10,
  modSetPoints: 0,
  modAdjustPoints: 0,
  viewerTransfers: 0
};

export function blankLoyaltyConfig(): LoyaltyConfig {
  return {
    pointsName: '',
    subPoints: 0,
    resubPoints: 0,
    giftSubPoints: 0,
    cheerPointsPer100: 0,
    watchPointsPerTick: 0,
    modSetPoints: 0,
    modAdjustPoints: 0,
    viewerTransfers: 0
  };
}

export type CounterScope = 'channel' | 'viewer' | 'command' | 'viewer_command';
export const COUNTER_SCOPES: readonly CounterScope[] = ['channel', 'viewer', 'command', 'viewer_command'];

export interface CounterDef {
  name: string;
  scope: CounterScope;
  value: string;
}

export interface CounterEntryView {
  viewerId: string;
  viewerLogin: string;
  viewerName: string;
  command: string;
  value: string;
}

export interface LoyaltyStanding {
  viewerId: string;
  viewerLogin: string;
  viewerName: string;
  points: number;
  watchSeconds: number;
}
