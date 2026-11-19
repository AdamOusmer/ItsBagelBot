// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { TargetAction } from './types';

export interface PollAction {
  title: string;
  choices: string[];
  seconds: number;
  points_per_vote?: number;
}

export interface PollEndAction {
  status: 'terminate' | 'archive';
}

export interface PredictionAction {
  title: string;
  outcomes: string[];
  seconds: number;
}

export interface PredictionEndAction {
  status: 'lock' | 'cancel' | 'resolve';
  winner?: number;
  outcome?: string;
}

export interface LoginToggleAction extends TargetAction {
  on?: boolean;
}

export interface UnbanResolveAction {
  status: 'approve' | 'deny';
  resolution?: string;
}

export interface BrandedContentAction {
  on?: boolean;
}

export type ChatModeValueRange = readonly [number, number];

export const CHAT_MODE_VALUES = {
  emote_only: [],
  followers: [[0, 129600]],
  slow: [[0, 0], [3, 120]],
  subscribers: [],
  unique: [],
  chat_delay: [[2, 2], [4, 4], [6, 6]],
} as const satisfies Record<string, readonly ChatModeValueRange[]>;

export const CHAT_MODE_NAMES = Object.keys(CHAT_MODE_VALUES);
export type ChatModeName = keyof typeof CHAT_MODE_VALUES;

export interface ChatModeAction {
  mode: ChatModeName;
  on?: boolean;
  value?: number;
}

export interface WhisperAction {
  login: string;
  text: string;
}
