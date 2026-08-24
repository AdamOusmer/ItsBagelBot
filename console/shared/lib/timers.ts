// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// One repeating chat message: stream-only (armed on stream.online, stopped on
// stream.offline; see sesame's ValkeyTimerStore). No Twitch-side entity, so
// unlike ChannelPointReward there is nothing to CRUD but this blob's own id.
export interface TimerDef {
  // id is a dashboard-generated id; empty only on an unsaved draft.
  id: string;
  message: string;
  intervalSeconds: number;
  enabled: boolean;
}

// blankTimer is the default draft for the "new timer" form.
export function blankTimer(): TimerDef {
  return { id: '', message: '', intervalSeconds: 600, enabled: true };
}
