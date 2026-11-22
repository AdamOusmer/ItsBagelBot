// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const DEFAULT_CHAT_WINDOW_MINUTES = 5;
export const DEFAULT_CHAT_LINES = 15;

export interface TimerDef {
  id: string;
  message: string;
  intervalSeconds: number;
  enabled: boolean;
  minChatLines: number;
  chatWindowMinutes: number;
  allowOffline: boolean;
  maxFiresPerStream: number;
  endsAt: string;
}

export function blankTimer(): TimerDef {
  return {
    id: '',
    message: '',
    intervalSeconds: 600,
    enabled: true,
    minChatLines: DEFAULT_CHAT_LINES,
    chatWindowMinutes: 10,
    allowOffline: false,
    maxFiresPerStream: 0,
    endsAt: ''
  };
}
