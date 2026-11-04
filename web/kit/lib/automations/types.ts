// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Params = Record<string, unknown>;

export interface Trigger {
  type: string;
  params?: Params;
}

export interface Condition {
  type: string;
  params?: Params;
}

export interface Action {
  type: string;
  enabled?: boolean;
  params?: Params;
}

export interface Definition {
  version: number;
  trigger: Trigger;
  conditions?: Condition[];
  actions?: Action[];
}

export interface Issue {
  path: string;
  code: string;
}

export interface Automation {
  id: string;
  user_id: number;
  name: string;
  enabled: boolean;
  revision: number;
  definition: Definition;
  created_at: string;
  updated_at: string;
}

export interface AutomationSummary {
  id: string;
  name: string;
  enabled: boolean;
  revision: number;
  trigger_type: string;
  preview?: string;
  updated_at: string;
}

export interface SummariesReply extends Refusal {
  automations: AutomationSummary[];
}

export type MatchMode ='start' | 'exact' | 'anywhere' | 'regex';
export type CommandSource = 'chat' | 'sub_message';
export type CompareOp = 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte';
export type PermLevel = 'everyone' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster';
export type AnnounceColor = '' | 'primary' | 'blue' | 'green' | 'orange' | 'purple';

export interface CommandTrigger {
  name: string;
  aliases?: string[];
  match?: MatchMode;
  case_sensitive?: boolean;
  sources?: CommandSource[];
}

export interface EventTrigger {
  kind: string;
  filters?: Record<string, string>;
}

export interface TimerTrigger {
  interval_s: number;
  random_jitter_s?: number;
  min_chat_lines?: number;
  offline_ok?: boolean;
}

export interface RewardTrigger {
  reward_id: string;
}

export type EmptyParams = Record<string, never>;

export interface PermCondition {
  level: PermLevel;
}

export interface UsersCondition {
  allow?: string[];
  deny?: string[];
  allow_ids?: string[];
  deny_ids?: string[];
}

export interface LiveCondition {
  state: 'live' | 'offline';
}

export interface CooldownCondition {
  global_s?: number;
  user_s?: number;
}

export interface ChanceCondition {
  percent: number;
}

export interface CounterCondition {
  name: string;
  op: CompareOp;
  value: number;
}

export interface PointsCondition {
  op: CompareOp;
  value: number;
}

export interface CompareCondition {
  left: string;
  op: 'gte' | 'lt' | 'eq' | 'contains';
  right?: string;
}

export interface ArgRegexCondition {
  pattern: string;
}

export interface CategoryCondition {
  allow?: string[];
  deny?: string[];
}

export interface TextAction {
  text: string;
}

export interface AnnounceAction {
  text: string;
  color?: AnnounceColor;
}

export interface TimeoutAction {
  login: string;
  seconds: number;
  reason?: string;
}

export interface BanAction {
  login: string;
  reason?: string;
}

export interface LoginAction {
  login: string;
}

export interface WarnAction {
  login: string;
  reason: string;
}

export interface ShieldAction {
  on: boolean;
}

export interface MarkerAction {
  description?: string;
}

export interface SetTitleAction {
  title: string;
}

export interface SetGameAction {
  game: string;
}

export interface SetTagsAction {
  tags: string;
}

export interface CommercialAction {
  seconds: 30 | 60 | 90 | 120 | 150 | 180;
}

export interface PointsAddAction {
  login?: string;
  amount: number;
}

export interface PointsSetAction {
  login?: string;
  amount: number;
}

export interface CounterAddAction {
  name: string;
  amount: number;
}

export interface CounterSetAction {
  name: string;
  value: number;
}

export interface RedemptionAction {
  status: 'fulfill' | 'cancel';
}

export interface DelayAction {
  ms: number;
}

export interface Branch {
  weight: number;
  actions?: Action[];
}

export interface RandomPickAction {
  branches: Branch[];
}

export interface RunAction {
  automation_id: string;
}

export interface IfAction {
  condition: Condition;
  then?: Action[];
  else?: Action[];
}

export interface DiscordPostAction {
  guild_id: string;
  channel_id: string;
  text: string;
}

export interface HTTPPostAction {
  url: string;
  body?: string;
}

export interface LightsAction {
  device: string;
  sku: string;
  device_name?: string;
  mode?: 'color' | 'scene';
  color?: string;
  scene_id?: string;
  scene_name?: string;
  scene_type?: string;
  scene_instance?: string;
  scene_value?: string;
}

export type RefusalCode = 'invalid' | 'not_found' | 'forbidden' | 'conflict' | 'unavailable' | 'internal';

export interface Refusal {
  error?: string;
  code?: RefusalCode;
}

export type RunOutcome = 'ok' | 'skipped' | 'partial' | 'failed' | 'limited';
export type ActionStatus = 'ok' | 'skipped' | 'unsupported' | 'failed' | 'limited';

export interface ActionRecord {
  index: number;
  type: string;
  status: ActionStatus;
  detail?: string;
}

export interface RunRecord {
  run_id: string;
  automation_id: string;
  name: string;
  trigger: string;
  source: string;
  at: string;
  duration_ms: number;
  outcome: RunOutcome;
  reason?: string;
  actions: ActionRecord[];
}

export interface ListRequest {
  user_id: string;
}

export interface ListReply extends Refusal {
  automations: Automation[];
}

export interface GetRequest {
  user_id: string;
  id: string;
}

export interface GetReply extends Refusal {
  automation?: Automation;
}

export interface UpsertRequest {
  user_id: string;
  id?: string;
  name: string;
  enabled: boolean;
  definition: Definition;
  expected_revision?: number;
}

export interface UpsertReply extends Refusal {
  automation?: Automation;
  issues?: Issue[];
  conflict?: boolean;
}

export interface DeleteRequest {
  user_id: string;
  id: string;
}

export interface DeleteReply extends Refusal {
  deleted: boolean;
}

export interface ValidateRequest {
  user_id: string;
  name: string;
  definition: Definition;
}

export interface ValidateReply extends Refusal {
  issues: Issue[];
}

export interface RunsRequest {
  broadcaster_id: string;
  automation_id?: string;
  limit?: number;
}

export interface RunsReply extends Refusal {
  runs: RunRecord[];
}

export type RunSource = 'api' | 'deck' | 'dashboard';

export interface RunRequest {
  broadcaster_id: string;
  name: string;
  args?: Record<string, string>;
  source: RunSource;
  request_id: string;
  token_id?: string;
}

export interface RunReply extends Refusal {
  accepted: boolean;
  run_id?: string;
}
