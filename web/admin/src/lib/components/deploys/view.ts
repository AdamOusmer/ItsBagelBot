// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Pure view helpers for the Deploys page: state to tone, progress to bar
// value, and the literal i18n key tables.
//
// The key tables are Records of literal strings rather than template keys
// (`admin.deploys.stage.${id}`) so a grep for a key finds its use, and so a
// stage or kind added to types.ts without copy fails the Record type here
// instead of rendering the raw key on the page.

import type {
  CheckState,
  DeployPRInfo,
  DeployRun,
  DeployRunSummary,
  DeployStage,
  FailureAction,
  NodePod,
  PodPhase,
  Progress,
  RunKind,
  RunState,
  StageId,
  StageState
} from '$lib/deploys/types';
import { TERMINAL_RUN_STATES } from '$lib/deploys/types';

export type Tone = 'neutral' | 'success' | 'warning' | 'error';

/** The slice of getI18n().t these helpers call. */
export type Translate = (key: string, params?: Record<string, string>) => string;

export const KIND_KEY: Record<RunKind, string> = {
  release: 'admin.deploys.kind.release',
  hotfix: 'admin.deploys.kind.hotfix',
  bump: 'admin.deploys.kind.bump',
  rollback: 'admin.deploys.kind.rollback',
  reapply: 'admin.deploys.kind.reapply'
};

export const KIND_HINT_KEY: Record<RunKind, string> = {
  release: 'admin.deploys.kindHint.release',
  hotfix: 'admin.deploys.kindHint.hotfix',
  bump: 'admin.deploys.kindHint.bump',
  rollback: 'admin.deploys.kindHint.rollback',
  reapply: 'admin.deploys.kindHint.reapply'
};

export const CONFIRM_KEY: Record<RunKind, string> = {
  release: 'admin.deploys.confirm.release',
  hotfix: 'admin.deploys.confirm.hotfix',
  bump: 'admin.deploys.confirm.bump',
  rollback: 'admin.deploys.confirm.rollback',
  reapply: 'admin.deploys.confirm.reapply'
};

export const STAGE_KEY: Record<StageId, string> = {
  preflight: 'admin.deploys.stage.preflight',
  merge_prs: 'admin.deploys.stage.merge_prs',
  changelog: 'admin.deploys.stage.changelog',
  tag: 'admin.deploys.stage.tag',
  release: 'admin.deploys.stage.release',
  build: 'admin.deploys.stage.build',
  digests: 'admin.deploys.stage.digests',
  pin_pr: 'admin.deploys.stage.pin_pr',
  acl: 'admin.deploys.stage.acl',
  rollout: 'admin.deploys.stage.rollout',
  verify: 'admin.deploys.stage.verify'
};

export const RUN_STATE_KEY: Record<RunState, string> = {
  running: 'admin.deploys.runState.running',
  waiting: 'admin.deploys.runState.waiting',
  succeeded: 'admin.deploys.runState.succeeded',
  failed: 'admin.deploys.runState.failed',
  cancelled: 'admin.deploys.runState.cancelled',
  verify_failed: 'admin.deploys.runState.verify_failed'
};

export const STEP_STATE_KEY: Record<StageState, string> = {
  pending: 'admin.deploys.stepState.pending',
  running: 'admin.deploys.stepState.running',
  waiting: 'admin.deploys.stepState.waiting',
  succeeded: 'admin.deploys.stepState.succeeded',
  failed: 'admin.deploys.stepState.failed',
  skipped: 'admin.deploys.stepState.skipped',
  cancelled: 'admin.deploys.stepState.cancelled'
};

export const POD_PHASE_KEY: Record<PodPhase, string> = {
  old: 'admin.deploys.pod.old',
  new: 'admin.deploys.pod.new',
  pending: 'admin.deploys.pod.pending',
  failing: 'admin.deploys.pod.failing'
};

export const CHECK_KEY: Record<CheckState, string> = {
  none: 'admin.deploys.check.none',
  pending: 'admin.deploys.check.pending',
  success: 'admin.deploys.check.success',
  failure: 'admin.deploys.check.failure'
};

const RUN_TONE: Record<RunState, Tone> = {
  running: 'neutral',
  waiting: 'warning',
  succeeded: 'success',
  failed: 'error',
  cancelled: 'neutral',
  verify_failed: 'error'
};

const STAGE_TONE: Record<StageState, Tone> = {
  pending: 'neutral',
  running: 'neutral',
  waiting: 'warning',
  succeeded: 'success',
  failed: 'error',
  skipped: 'neutral',
  cancelled: 'neutral'
};

const CHECK_TONE: Record<CheckState, Tone> = {
  none: 'neutral',
  pending: 'warning',
  success: 'success',
  failure: 'error'
};

const POD_TONE: Record<PodPhase, Tone> = {
  old: 'neutral',
  new: 'success',
  pending: 'warning',
  failing: 'error'
};

// StatePill speaks the admin's domain tones rather than StatusTone.
const PILL: Record<Tone, 'neutral' | 'positive' | 'warning' | 'danger'> = {
  neutral: 'neutral',
  success: 'positive',
  warning: 'warning',
  error: 'danger'
};

export const runTone = (s: RunState): Tone => RUN_TONE[s];
export const runPill = (s: RunState) => PILL[RUN_TONE[s]];
export const stageTone = (s: StageState): Tone => STAGE_TONE[s];
export const checkTone = (s: CheckState): Tone => CHECK_TONE[s];
export const podTone = (p: PodPhase): Tone => POD_TONE[p];

export const isTerminal = (s: RunState): boolean => TERMINAL_RUN_STATES.includes(s);

/** done/total, or undefined while the total is not known yet. */
export function ratio(p: Progress | undefined): number | undefined {
  if (!p || p.total <= 0) return undefined;
  return p.done / p.total;
}

/**
 * The StepList bar for one stage. undefined leaves the slot empty (a stage
 * that has not started, or one that was skipped, has nothing to measure);
 * null is the indeterminate sweep for a stage that is moving without a total
 * yet, such as a build whose jobs are still queued.
 */
export function stageValue(s: Pick<DeployStage, 'state' | 'progress'>): number | null | undefined {
  switch (s.state) {
    case 'succeeded':
      return 1;
    case 'pending':
    case 'skipped':
      return undefined;
    case 'running':
    case 'waiting':
      return ratio(s.progress) ?? null;
    default:
      return ratio(s.progress) ?? 0;
  }
}

function stageShare(s: DeployStage): number {
  if (s.state === 'succeeded' || s.state === 'skipped') return 1;
  return ratio(s.progress) ?? 0;
}

/** Whole-run share: finished stages count 1, the moving one its own fraction. */
export function runFraction(run: Pick<DeployRun, 'stages'>): number {
  if (run.stages.length === 0) return 0;
  return run.stages.reduce((sum, s) => sum + stageShare(s), 0) / run.stages.length;
}

const pad = (n: number) => String(n).padStart(2, '0');

/** m:ss under an hour, h:mm:ss past it. Negative spans (clock skew) read 0:00. */
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = pad(total % 60);
  return h > 0 ? `${h}:${pad(m)}:${s}` : `${m}:${s}`;
}

/** Elapsed run time; a finished run stops at its last update. */
export function runElapsed(run: Pick<DeployRun, 'created_at' | 'updated_at' | 'state'>, now: number): number {
  const end = isTerminal(run.state) ? new Date(run.updated_at).getTime() : now;
  return end - new Date(run.created_at).getTime();
}

// A run merges a PR only after its required checks pass, and it waits on
// the ones still running. A failed or absent check would park the run on a
// wait that never ends, so those rows cannot be ticked at all.
const MOVING: ReadonlySet<CheckState> = new Set(['success', 'pending']);

export function tickable(pr: DeployPRInfo): boolean {
  if (pr.draft) return false;
  return MOVING.has(pr.checks) && MOVING.has(pr.codescene);
}

/** First 7 of a commit sha, the length GitHub prints. */
export const shortSha = (sha: string | undefined): string => (sha ?? '').slice(0, 7);

/**
 * Run.CurrentStage from deploy.go: the first stage not yet succeeded or
 * skipped. A run with every stage done answers its last stage.
 */
export function currentStageId(run: Pick<DeployRun, 'stages'>): StageId {
  const open = run.stages.find((s) => s.state !== 'succeeded' && s.state !== 'skipped');
  return (open ?? run.stages[run.stages.length - 1])?.id ?? 'preflight';
}

/** The stage a run stopped on, for the banner and the retry verb. */
export function failedStage(run: DeployRun): DeployStage | undefined {
  return run.stages.find((s) => s.state === 'failed');
}

export function approvalStage(run: DeployRun): DeployStage | undefined {
  return run.stages.find((s) => Boolean(s.needs_approval));
}

/** What the version column shows: the version when the kind has one. */
export function runName(run: Pick<DeployRunSummary, 'version' | 'target_sha'>): string {
  return run.version ?? shortSha(run.target_sha);
}

/**
 * The line under a stage's label. An approval or a wait is what the operator
 * has to act on or explain, so either one replaces the count.
 */
export function stageMeta(s: DeployStage, t: Translate): string {
  if (s.needs_approval) return t('admin.deploys.needsApproval', { reason: s.needs_approval });
  if (s.waiting) return s.waiting;
  const parts: string[] = [];
  if (s.progress.total > 0) parts.push(`${s.progress.done}/${s.progress.total}`);
  if ((s.attempts ?? 0) > 1) parts.push(t('admin.deploys.attempt', { n: String(s.attempts) }));
  return parts.join(' · ');
}

/** Rollout pods grouped per node, nodes in first-seen order. */
export function groupNodes(pods: NodePod[] | undefined): { node: string; pods: NodePod[] }[] {
  const byNode = new Map<string, NodePod[]>();
  for (const p of pods ?? []) byNode.set(p.node, [...(byNode.get(p.node) ?? []), p]);
  return [...byNode].map(([node, list]) => ({ node, pods: list }));
}

/**
 * The verbs the deployer offers for how a run stopped. They come from the
 * failure itself (the engine fills defaults per stage), so the page offers
 * exactly what the deployer will accept rather than guessing from the state.
 */
export function offered(run: DeployRun): ReadonlySet<FailureAction> {
  return new Set(run.failure?.actions ?? failedStage(run)?.failure?.actions ?? []);
}

/**
 * A moving run takes a cancel request (it stops at the next safe point); a
 * stopped one is cancelled only when its failure offers it, which is how the
 * engine abandons a run so it can no longer be resumed.
 */
export function cancellable(run: DeployRun, acts: ReadonlySet<FailureAction>): boolean {
  if (acts.has('cancel')) return true;
  return !isTerminal(run.state) && !run.cancel_requested;
}
