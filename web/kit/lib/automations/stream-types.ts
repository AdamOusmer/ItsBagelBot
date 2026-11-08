// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface PollAction {
  title: string;
  choices: string[];
  seconds: number;
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
}

export interface LoginToggleAction {
  login: string;
  on?: boolean;
}

export interface UnbanResolveAction {
  status: 'approve' | 'deny';
  resolution?: string;
}

export interface BrandedContentAction {
  on?: boolean;
}

export type ChatModeName = 'emote_only' | 'followers' | 'slow' | 'subscribers' | 'unique';

export interface ChatModeAction {
  mode: ChatModeName;
  on?: boolean;
  value?: number;
}

export interface WhisperAction {
  login: string;
  text: string;
}
