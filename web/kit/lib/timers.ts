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
  // Gate: skip a tick unless at least this many chat lines arrived since the
  // timer last fired. 0 = off. See docs/specs/timer-conditions.md D2-D4.
  minChatLines: number;
  // Stop: do not re-arm once this many posts have fired this stream. 0 =
  // unlimited. See docs/specs/timer-conditions.md D5.
  maxFiresPerStream: number;
  // Stop: RFC 3339 UTC instant past which the timer neither fires nor
  // re-arms. '' = never. See docs/specs/timer-conditions.md D6-D7.
  endsAt: string;
}

// blankTimer is the default draft for the "new timer" form.
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
