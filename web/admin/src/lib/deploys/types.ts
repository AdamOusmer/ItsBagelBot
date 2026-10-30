// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const RUN_KINDS = ['release', 'hotfix', 'bump', 'rollback', 'reapply'] as const;
export type RunKind = (typeof RUN_KINDS)[number];

export const STAGE_IDS = [
  'preflight',
  'merge_prs',
  'changelog',
  'tag',
  'release',
  'build',
  'digests',
  'pin_pr',
  'promote',
] as const;
export type StageId = (typeof STAGE_IDS)[number];

export const STAGES_FOR: Record<RunKind, readonly StageId[]> = {
  release: ['preflight', 'merge_prs', 'changelog', 'tag', 'release', 'build', 'digests', 'pin_pr', 'promote'],
  hotfix: ['preflight', 'merge_prs', 'changelog', 'tag', 'release', 'build', 'digests', 'pin_pr', 'promote'],
  bump: ['preflight', 'merge_prs', 'build', 'digests', 'pin_pr', 'promote'],
  rollback: ['preflight', 'pin_pr', 'promote'],
  reapply: ['preflight', 'promote'],
};

export type StageState = 'pending' | 'running' | 'waiting' | 'succeeded' | 'failed' | 'skipped' | 'cancelled';

export type RunState = 'running' | 'waiting' | 'succeeded' | 'failed' | 'cancelled' | 'verify_failed';

export const TERMINAL_RUN_STATES: readonly RunState[] = ['succeeded', 'failed', 'cancelled', 'verify_failed'];

export type Progress = { done: number; total: number };

export type DeployItem = {
  key: string;
  label: string;
  state: StageState;
  progress: Progress;
  detail?: string;
  url?: string;
};

export type DeployLink = { label: string; url: string };

export type FailureCode =
  | 'lock_held'
  | 'cluster_unreachable'
  | 'not_settled'
  | 'checks_failed'
  | 'lint_refused'
  | 'not_mergeable'
  | 'behind_limit'
  | 'build_failed'
  | 'digest_refused'
  | 'apply_refused'
  | 'apply_failed'
  | 'unschedulable'
  | 'crash_loop'
  | 'image_pull'
  | 'timeout'
  | 'verify_failed'
  | 'github'
  | 'kube'
  | 'internal';

export type FailureAction = 'resume' | 'rerun_failed_jobs' | 'rollback' | 'cancel';

export type DeployFailure = {
  code: FailureCode;
  message: string;
  log_tail?: string[];
  actions?: FailureAction[];
};

export type DeployStage = {
  id: StageId;
  state: StageState;
  progress: Progress;
  started_at?: string;
  ended_at?: string;
  items?: DeployItem[];
  links?: DeployLink[];
  failure?: DeployFailure;
  waiting?: string;
  needs_approval?: string;
  attempts?: number;
};

export type ImagePin = { tag: string; digest: string };

export type DeployOutputs = {
  live_sha?: string;
  live_version?: string;
  merged_prs?: number[];
  changelog_pr?: number;
  changelog_sha?: string;
  tag_sha?: string;
  release_url?: string;
  build_run_id?: number;
  build_run_url?: string;
  build_run_attempt?: number;
  digests?: Record<string, ImagePin>;
  services?: string[];
  pin_pr?: number;
  pin_sha?: string;
  messaging_changed: boolean;
  acl_applied_at?: string;
};

export type DeployActor = { id: string; login: string };

export type ChangelogEntry = {
  title: Record<string, string>;
  highlights: Record<string, string[]>;
  date?: string;
};

export type DeployRun = {
  id: string;
  seq: number;
  kind: RunKind;
  version?: string;
  target_sha?: string;
  prs?: number[];
  changelog?: ChangelogEntry;
  rollback_to?: string;
  services?: string[];
  actor: DeployActor;
  state: RunState;
  stages: DeployStage[];
  outputs: DeployOutputs;
  failure?: DeployFailure;
  cancel_requested?: boolean;
  created_at: string;
  updated_at: string;
};

export type DeployRunSummary = {
  id: string;
  kind: RunKind;
  version?: string;
  target_sha?: string;
  state: RunState;
  current_stage?: StageId;
  actor: DeployActor;
  created_at: string;
  updated_at: string;
};

export type CheckState = 'none' | 'pending' | 'success' | 'failure';

export type DeployPRInfo = {
  number: number;
  title: string;
  author: string;
  url: string;
  head_sha: string;
  draft: boolean;
  checks: CheckState;
  codescene: CheckState;
  mergeable: boolean;
  behind: boolean;
};

export type DeployCommit = { sha: string; title: string; author: string; pr?: number; url?: string };

export type DeployDriftItem = {
  namespace: string;
  workload: string;
  container: string;
  pinned: string;
  live: string;
};

export type DeployReleaseInfo = { version: string; sha: string; url: string; published_at: string };

export type DeployPlan = {
  live_version?: string;
  live_sha?: string;
  last_tag?: string;
  next_version?: string;
  main_sha?: string;
  in_sync: boolean;
  drift?: DeployDriftItem[];
  commits?: DeployCommit[];
  prs?: DeployPRInfo[];
  releases?: DeployReleaseInfo[];
  services?: string[];
  active_run_id?: string;
};

export type ImageOverride = { name: string; newTag?: string; digest?: string };

export type FluxTier = {
  name: string;
  ready: boolean | null;
  reason: string;
  message: string;
  revision: string;
  suspended: boolean;
  dependsOn: string[];
  images: ImageOverride[];
};

export type FluxResult = { tiers: FluxTier[]; pinnable: string[]; fluxError: string | null };

export type PlanRequest ={ actor_id: string; kind?: RunKind; rollback_to?: string };

export type StartRequest = {
  actor_id: string;
  kind: RunKind;
  version?: string;
  target_sha?: string;
  prs?: number[];
  changelog?: ChangelogEntry;
  rollback_to?: string;
  services?: string[];
};

export type RunRequest = { actor_id: string; run_id: string; stage?: StageId; rerun?: boolean };

export type ListRequest = { actor_id: string; limit?: number };

export type Refusal = { error?: string; code?: string };

export type PlanReply = Refusal & { plan?: DeployPlan };
export type RunReply = Refusal & { run?: DeployRun };
export type ListReply = Refusal & { runs: DeployRunSummary[]; active_run_id?: string };
