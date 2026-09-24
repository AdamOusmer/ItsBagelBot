// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CounterScope } from './loyalty';

export type RewardActionKind = 'chat' | 'none';
export type RewardOnRedeem = 'fulfill' | 'cancel' | 'leave';

export const REWARD_ACTIONS: readonly RewardActionKind[] = ['chat', 'none'];
export const REWARD_ON_REDEEM: readonly RewardOnRedeem[] = ['fulfill', 'cancel', 'leave'];

export interface ChannelPointReward {
  id: string;
  title: string;
  cost: number;
  prompt: string;
  backgroundColor: string;
  isEnabled: boolean;
  isPaused: boolean;
  isUserInputRequired: boolean;
  maxPerStreamEnabled: boolean;
  maxPerStream: number;
  maxPerUserPerStreamEnabled: boolean;
  maxPerUserPerStream: number;
  globalCooldownEnabled: boolean;
  globalCooldownSeconds: number;
  action: RewardActionKind;
  message: string;
  onRedeem: RewardOnRedeem;
  counter: string;
  counterScope: CounterScope;
  points: number;
  liveOnly: boolean;
}

export function blankReward(): ChannelPointReward {
  return {
    id: '',
    title: '',
    cost: 100,
    prompt: '',
    backgroundColor: '',
    isEnabled: true,
    isPaused: false,
    isUserInputRequired: false,
    maxPerStreamEnabled: false,
    maxPerStream: 1,
    maxPerUserPerStreamEnabled: false,
    maxPerUserPerStream: 1,
    globalCooldownEnabled: false,
    globalCooldownSeconds: 60,
    action: 'chat',
    message: '',
    onRedeem: 'fulfill',
    counter: '',
    counterScope: 'viewer_command',
    points: 0,
    liveOnly: false
  };
}
