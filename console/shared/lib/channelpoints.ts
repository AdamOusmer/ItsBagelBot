// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CounterScope } from './loyalty';

// --- Channel points -------------------------------------------------------
// The Channel Points tab (its own dashboard section, NOT a module tile) lets a
// broadcaster create Twitch custom rewards (created under the bot's client id,
// styled natively on Twitch) and bind each one to a bot action that runs when a
// viewer redeems it. The Twitch-side reward is owned by outgress (broadcaster
// token); the action binding is stored in the hidden "channelpoints" module blob
// and read by sesame's channelpoints module.

// The bot action a redemption triggers. 'chat' posts the reward's message;
// 'none' manages the reward only, leaving just the resolution policy to act.
export type RewardActionKind = 'chat' | 'none';
// What to do with the redemption in Twitch's request queue after the action:
// mark it fulfilled, cancel (refund the points), or leave it for a human mod.
export type RewardOnRedeem = 'fulfill' | 'cancel' | 'leave';

export const REWARD_ACTIONS: readonly RewardActionKind[] = ['chat', 'none'];
export const REWARD_ON_REDEEM: readonly RewardOnRedeem[] = ['fulfill', 'cancel', 'leave'];

// One channel-points reward as the dashboard works with it: the Twitch reward
// fields plus the local action binding, merged into a single row.
export interface ChannelPointReward {
  // id is the Twitch-assigned custom reward id; empty only on an unsaved draft.
  id: string;
  title: string;
  cost: number;
  prompt: string;
  backgroundColor: string;
  isEnabled: boolean;
  isPaused: boolean;
  isUserInputRequired: boolean;
  // Limit controls ("claimable once and so on").
  maxPerStreamEnabled: boolean;
  maxPerStream: number;
  maxPerUserPerStreamEnabled: boolean;
  maxPerUserPerStream: number;
  globalCooldownEnabled: boolean;
  globalCooldownSeconds: number;
  // Local action binding (stored in the channelpoints module blob, read by sesame).
  action: RewardActionKind;
  message: string;
  onRedeem: RewardOnRedeem;
  // Loyalty hooks: counter names a loyalty counter bumped once per redemption
  // (the reward title keys a per-user+command counter's bucket); points, when
  // positive, awards that many loyalty points to the redeemer.
  counter: string;
  // counterScope is the scope to CREATE the counter with when it doesn't exist
  // yet, so a broadcaster can make the counter straight from the reward editor
  // instead of the Counters page. Ignored when counter is empty, or when the
  // counter already exists (create is idempotent — it never changes a stored
  // scope). Defaults to per user + reward, the scope a reward-linked counter
  // almost always wants.
  counterScope: CounterScope;
  points: number;
  // liveOnly gates the loyalty writes (counter bump + points award) to when
  // the broadcaster is live, so channel points redeemed offline can't farm
  // currency or inflate a counter. The chat reply always runs.
  liveOnly: boolean;
}

// blankReward is the default draft for the "new reward" form.
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
