// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type ScenarioValue = string | number | boolean;
export type RehearsalRole = 'viewer' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster';
export type RehearsalState = {
  channelVars: Record<string, string>;
  viewerVars: Record<string, string>;
  counters: Record<string, number>;
  points: number;
  cooldowns: Record<string, number>;
  countdowns: Record<string, number>;
};

export type RehearsalScenario = {
  user?: string;
  userId?: string;
  role?: RehearsalRole;
  live: boolean;
  message?: string;
  args?: string;
  bits?: number;
  viewers?: number;
  tier?: string;
  months?: number;
  giftCount?: number;
  anonymous?: boolean;
  input?: string;
  requestText?: string;
  is_gift?: boolean;
  reward_id?: string;
  name?: string;
  values?: Record<string, string>;
  state?: Partial<RehearsalState>;
};

export type ScenarioField = {
  key: keyof RehearsalScenario;
  kind: 'text' | 'number' | 'role' | 'select' | 'toggle';
  default: ScenarioValue;
  more?: boolean;
  options?: readonly string[];
};

export type RehearsalOverrides = Readonly<Record<string, string>>;
export type RehearsalReason = { reason: string; values: Record<string, string> };
export type TriggerMatch = { starts: true } | ({ starts: false } & RehearsalReason);

export type RehearsalEvent = {
  key: string;
  path: string;
  lane: number[];
  iteration: number[];
  atMs: number;
  kind: 'trigger' | 'check' | 'say' | 'effect' | 'wait' | 'parallel' | 'loop' | 'skip';
  status: 'ok' | 'failed' | 'skipped' | 'yes' | 'no';
  port?: string;
  lines?: string[];
  detail?: Record<string, string>;
};

export type RehearsalResult = {
  starts: boolean;
  reason?: RehearsalReason;
  events: RehearsalEvent[];
};

export type RehearsalContext = {
  scenario: RehearsalScenario;
  state: RehearsalState;
  overrides: RehearsalOverrides;
  locals: Record<string, string>;
  cooldownClaims?: Record<string, number>;
  path: string;
  atMs: number;
};
