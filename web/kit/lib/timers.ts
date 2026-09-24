// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface TimerDef {
  id: string;
  message: string;
  intervalSeconds: number;
  enabled: boolean;
  minChatLines: number;
  maxFiresPerStream: number;
  endsAt: string;
}

export function blankTimer(): TimerDef {
  return {
    id: '',
    message: '',
    intervalSeconds: 600,
    enabled: true,
    minChatLines: 0,
    maxFiresPerStream: 0,
    endsAt: ''
  };
}
