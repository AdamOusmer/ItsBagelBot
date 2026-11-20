// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RehearsalMode } from '../engine/rehearsal';

export type ScenarioValue = string | number | boolean;
export type RehearsalRole = 'viewer' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster';
export type RehearsalState = {
  channelVars: Record<string, string>;
  viewerVars: Record<string, string>;
  counters: Record<string, number>;
  points: number;
  cooldowns: Record<string, number>;
  countdowns: Record<string, number>;
  running: Record<'poll' | 'prediction', boolean>;
};

export type RehearsalScenario = {
  user?: string;
  userId?: string;
  role?: RehearsalRole;
  live: boolean;
  game?: string;
  message?: string;
  args?: string;
  bits?: number;
  viewers?: number;
  tier?: string;
  months?: number;
  streak?: number;
  giftCount?: number;
  total?: number;
  anonymous?: boolean;
  input?: string;
  requestText?: string;
  isGift?: boolean;
  rewardId?: string;
  reward?: string;
  cost?: number;
  title?: string;
  category?: string;
  seconds?: number;
  automatic?: boolean;
  name?: string;
  values?: Record<string, string>;
  state?: Partial<Omit<RehearsalState, 'running'>>;
};

export type ScenarioField = {
  key: Exclude<keyof RehearsalScenario, 'state' | 'values'>;
  kind: 'text' | 'number' | 'role' | 'select' | 'toggle';
  default: ScenarioValue;
  options?: readonly string[];
};

export type ScenarioStateField = { key: `state:${string}` } & (
  | { seed: 'points'; kind: 'number'; default: number }
  | { seed: 'counters'; name: string; kind: 'number'; default: number }
  | { seed: 'countdowns' | 'cooldowns'; name: string } & (
    | { kind: 'toggle'; default: false; initialSeconds: number }
    | { kind: 'duration'; default: number }
  )
);
export type ScenarioInputField = ScenarioField | ScenarioStateField;

export type RehearsalOverrides = Readonly<Record<string, string>>;
export type RehearsalReason = { reason: string; values: Record<string, string> };
export type TriggerMatch = { starts: true } | ({ starts: false } & RehearsalReason);
export type ConditionOutcome = { passes: boolean; code?: string };

export type RehearsalLineStyle = { mode: RehearsalMode; color?: string; target?: string };

export type RehearsalEvent = {
  key: string;
  path: string;
  lane: number[];
  iteration: number[];
  atMs: number;
  kind: 'trigger' | 'check' | 'say' | 'effect' | 'wait' | 'parallel' | 'loop' | 'skip' | 'lights_restore';
  status: 'ok' | 'failed' | 'skipped' | 'yes' | 'no';
  port?: string;
  lines?: string[];
  lineStyles?: RehearsalLineStyle[];
  detail?: Record<string, string>;
};

export type RehearsalResult = {
  starts: boolean;
  reason?: RehearsalReason;
  events: RehearsalEvent[];
};

export type RehearsalContext = {
  triggerType: string;
  scenario: RehearsalScenario;
  hasChatMessage: boolean;
  state: RehearsalState;
  cooldownOrdinals: ReadonlyMap<string, number>;
  overrides: RehearsalOverrides;
  locals: Record<string, string>;
  path: string;
  atMs: number;
  formatDuration?: (seconds: number) => string;
};
