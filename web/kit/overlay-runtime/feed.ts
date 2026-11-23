// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const alertKinds = ['follow', 'sub', 'resub', 'gift_sub', 'community_gift', 'cheer', 'raid', 'redemption'] as const;
export const eventKinds = [...alertKinds, 'chat', 'user_cleared'] as const;
export type AlertKind = (typeof alertKinds)[number];
export type EventKind = (typeof eventKinds)[number];
export type Tier = '1000' | '2000' | '3000' | 'prime' | '';
export type Identity = { id?: string; login?: string; name?: string; color?: string };
export type Part = { t: 'text' | 'emote'; text?: string; id?: string };
export type OverlayEvent = {
  id: string;
  kind: EventKind;
  at: string;
  user?: Identity;
  anonymous?: boolean;
  upgrade?: boolean;
  amount?: number;
  tier?: Tier;
  months?: number;
  streak?: number;
  recipient?: Identity;
  message?: { text?: string; parts?: Part[] };
  badges?: string[];
  reward?: { id?: string; title?: string };
};
export type FeedEvent = OverlayEvent & { seq: number; test?: boolean; replay?: boolean };
export type Counts = {
  follows?: number;
  subs?: number;
  resubs?: number;
  gifts?: number;
  bits?: number;
  raids?: number;
  redemptions?: number;
  sub_points?: number;
  tiers?: Partial<Record<Tier, number>>;
};
export type Leader = Identity & { amount: number };
export type CreditList = AlertKind | 'mod' | 'vip' | 'chatter';
export type FeedStats = {
  session?: Counts;
  week?: Counts;
  month?: Counts;
  all_time?: Counts;
  latest?: Partial<Record<EventKind, OverlayEvent>>;
  top_cheerer?: Leader;
  top_gifter?: Leader;
  all_time_top_cheerer?: Leader;
  all_time_top_gifter?: Leader;
  credits?: Partial<Record<CreditList, Leader[]>>;
};
export type AlertControl = { paused: boolean; muted: boolean; skip: number };
export type Subathon = { running?: boolean; end_at?: string; remaining_seconds?: number };
export type FeedSnapshot = {
  stats: FeedStats;
  subathon: Subathon;
  control: AlertControl;
  chat?: FeedEvent[] | null;
  events?: FeedEvent[] | null;
};
export type FeedMessages = {
  snapshot: FeedSnapshot;
  event: OverlayEvent;
  stats: FeedStats;
  subathon: Subathon;
  control: AlertControl;
};
