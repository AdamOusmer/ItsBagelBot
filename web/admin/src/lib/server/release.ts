// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  STAGES_FOR,
  type CheckState,
  type DeployCommit,
  type DeployFailure,
  type DeployPlan,
  type DeployPRInfo,
  type DeployRun,
  type DeployRunSummary,
  type DeployStage,
  type ListRequest,
  type PlanRequest,
  type RunKind,
  type RunRequest,
  type RunState,
  type StageId,
  type StageState,
  type StartRequest
} from '#lib/deploys/types';
import { github, githubMaybe, GitHubError } from './github';

const WORKFLOW = 'release.yml';
const MAIN = 'main';
const DEPLOY_BRANCH = 'deploy';
const CODESCENE = 'CodeScene Code Health Review (main)';
const WATCH_EVERY_MS = 4_000;
const FIND_DISPATCHED_TRIES = 10;
const PLAN_PRS = 20;

export type DeployRunId = DeployRun['id'];
export type DeployRuns = { runs: DeployRunSummary[]; activeRunId: DeployRunId | null };
export type DeployListener = { run: (run: DeployRun) => void; gap: () => void };

type WorkflowRun = {
  id: number;
  display_title: string;
  status: string;
  conclusion: string | null;
  created_at: string;
  updated_at: string;
  html_url: string;
  head_sha: string;
};

type Job = {
  name: string;
  status: string;
  conclusion: string | null;
  started_at: string | null;
  completed_at: string | null;
  html_url: string;
};

const ACTIVE = new Set(['queued', 'in_progress', 'waiting', 'requested', 'pending']);

const CONCLUSION: Record<string, StageState> = {
  success: 'succeeded',
  skipped: 'skipped',
  cancelled: 'cancelled'
};

type Title = { kind: RunKind; version?: string; login: string };

function parseTitle(title: string): Title {
  const [head, login = ''] = title.split(' by ');
  const [kind, version] = head.trim().split(/\s+/);
  return { kind: (STAGES_FOR[kind as RunKind] ? kind : 'release') as RunKind, version: version || undefined, login };
}

function runState(run: WorkflowRun): RunState {
  if (run.status === 'waiting') return 'waiting';
  if (ACTIVE.has(run.status)) return 'running';
  if (run.conclusion === 'success') return 'succeeded';
  if (run.conclusion === 'cancelled') return 'cancelled';
  return 'failed';
}

function jobState(job: Job | undefined, runDone: boolean): StageState {
  if (!job) return runDone ? 'skipped' : 'pending';
  if (job.status === 'waiting') return 'waiting';
  if (job.status === 'queued' || job.status === 'pending') return 'pending';
  if (job.status !== 'completed') return 'running';
  return CONCLUSION[job.conclusion ?? ''] ?? 'failed';
}

function failureOf(id: StageId, job: Job): DeployFailure {
  return {
    code: 'github',
    message: `${id} ${job.conclusion ?? 'failed'}`,
    actions: id === 'promote' ? ['resume', 'rollback', 'cancel'] : ['resume', 'cancel']
  };
}

function stageOf(id: StageId, job: Job | undefined, runDone: boolean): DeployStage {
  const state = jobState(job, runDone);
  return {
    id,
    state,
    progress: { done: state === 'succeeded' ? 1 : 0, total: 1 },
    started_at: job?.started_at ?? undefined,
    ended_at: job?.completed_at ?? undefined,
    links: job ? [{ label: 'GitHub', url: job.html_url }] : undefined,
    failure: state === 'failed' && job ? failureOf(id, job) : undefined,
    waiting: state === 'waiting' ? 'waiting for the production environment review on GitHub' : undefined
  };
}

function toSummary(run: WorkflowRun): DeployRunSummary {
  const title = parseTitle(run.display_title);
  return {
    id: String(run.id),
    kind: title.kind,
    version: title.version,
    target_sha: run.head_sha,
    state: runState(run),
    actor: { id: '', login: title.login },
    created_at: run.created_at,
    updated_at: run.updated_at
  };
}

function toRun(run: WorkflowRun, jobs: Job[]): DeployRun {
  const summary = toSummary(run);
  const done = !ACTIVE.has(run.status);
  const byName = new Map(jobs.map((j) => [j.name, j]));
  const stages = STAGES_FOR[summary.kind].map((id) => stageOf(id, byName.get(id), done));
  return {
    ...summary,
    seq: Date.parse(run.updated_at) + jobs.filter((j) => j.status === 'completed').length,
    stages,
    outputs: { messaging_changed: false },
    failure: stages.find((s) => s.failure)?.failure
  };
}

async function workflowRuns(perPage: number): Promise<WorkflowRun[]> {
  const body = await github<{ workflow_runs: WorkflowRun[] }>(`/actions/workflows/${WORKFLOW}/runs?per_page=${perPage}`);
  return body.workflow_runs;
}

async function get(req: RunRequest): Promise<DeployRun> {
  const [run, jobs] = await Promise.all([
    github<WorkflowRun>(`/actions/runs/${encodeURIComponent(req.run_id)}`),
    github<{ jobs: Job[] }>(`/actions/runs/${encodeURIComponent(req.run_id)}/jobs?per_page=50`)
  ]);
  return toRun(run, jobs.jobs);
}

async function list(req: ListRequest): Promise<DeployRuns> {
  const runs = await workflowRuns(req.limit ?? 30);
  const active = runs.find((r) => ACTIVE.has(r.status));
  return { runs: runs.map(toSummary), activeRunId: active ? String(active.id) : null };
}

// Must render exactly like the workflow's run-name, empty version included.
function titleOf(req: StartRequest, login: string): string {
  return `${req.kind} ${req.version || req.rollback_to || ''} by ${login}`;
}

async function findDispatched(title: string, since: number): Promise<WorkflowRun> {
  for (let i = 0; i < FIND_DISPATCHED_TRIES; i++) {
    const run = (await workflowRuns(10)).find((r) => r.display_title === title && Date.parse(r.created_at) >= since);
    if (run) return run;
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
  throw new GitHubError(504, 'the release workflow was dispatched but its run has not appeared yet');
}

async function start(req: StartRequest & { actor_login?: string }): Promise<DeployRun> {
  const login = req.actor_login || req.actor_id;
  const since = Date.now() - 5_000;
  await github(`/actions/workflows/${WORKFLOW}/dispatches`, {
    method: 'POST',
    body: {
      ref: MAIN,
      inputs: {
        kind: req.kind,
        version: req.version ?? '',
        rollback_to: req.rollback_to ?? '',
        target_sha: req.target_sha ?? '',
        prs: (req.prs ?? []).join(','),
        services: (req.services ?? []).join(','),
        changelog: req.changelog ? JSON.stringify(req.changelog) : '',
        actor: login
      }
    }
  });
  const run = await findDispatched(titleOf(req, login), since);
  return get({ actor_id: req.actor_id, run_id: String(run.id) });
}

async function resume(req: RunRequest): Promise<DeployRun> {
  await github(`/actions/runs/${encodeURIComponent(req.run_id)}/rerun-failed-jobs`, { method: 'POST' });
  return get(req);
}

async function cancel(req: RunRequest): Promise<DeployRun> {
  await github(`/actions/runs/${encodeURIComponent(req.run_id)}/cancel`, { method: 'POST' });
  return get(req);
}

async function approve(): Promise<DeployRun> {
  throw new GitHubError(409, 'approve the production environment on the GitHub run page');
}

type Watch = { listeners: Set<DeployListener>; timer: ReturnType<typeof setInterval> };

const watches = new Map<DeployRunId, Watch>();

async function poll(runId: DeployRunId, w: Watch): Promise<void> {
  try {
    const run = await get({ actor_id: '', run_id: runId });
    w.listeners.forEach((l) => l.run(run));
  } catch {
    w.listeners.forEach((l) => l.gap());
  }
}

function watch(runId: DeployRunId, listener: DeployListener): () => void {
  let w = watches.get(runId);
  if (!w) {
    const created: Watch = { listeners: new Set(), timer: setInterval(() => void poll(runId, created), WATCH_EVERY_MS) };
    watches.set(runId, created);
    w = created;
  }
  w.listeners.add(listener);
  const current = w;
  return () => {
    current.listeners.delete(listener);
    if (current.listeners.size > 0) return;
    clearInterval(current.timer);
    watches.delete(runId);
  };
}

type Ref = { object: { sha: string } };
type Branch = { commit: { sha: string } };
type Release = { tag_name: string; target_commitish: string; html_url: string; published_at: string };
type CompareCommit = { sha: string; html_url: string; commit: { message: string; author: { name: string } } };
type Compare = { commits: CompareCommit[]; files?: { filename: string }[] };
type Pull = { number: number; title: string; html_url: string; draft: boolean; user: { login: string }; head: { sha: string } };
type PullDetail = { mergeable: boolean | null; mergeable_state: string };
type CheckRun = { name: string; status: string; conclusion: string | null };

const BETA = /^v(\d+)\.(\d+)\.(\d+)-beta$/;

function semver(tag: string): number[] | null {
  const m = BETA.exec(tag);
  return m ? m.slice(1).map(Number) : null;
}

type Tagged = { tag: string; v: number[] };

function byVersionDesc(a: Tagged, b: Tagged): number {
  return b.v[0] - a.v[0] || b.v[1] - a.v[1] || b.v[2] - a.v[2];
}

function tagged(ref: { ref: string }): Tagged[] {
  const tag = ref.ref.replace('refs/tags/', '');
  const v = semver(tag);
  return v ? [{ tag, v }] : [];
}

async function lastTag(): Promise<string | undefined> {
  const refs = await github<{ ref: string }[]>('/git/matching-refs/tags/v');
  return refs.flatMap(tagged).sort(byVersionDesc)[0]?.tag;
}

function nextVersion(tag: string | undefined): string | undefined {
  const v = tag ? semver(tag) : null;
  return v ? `v${v[0]}.${v[1]}.${v[2] + 1}-beta` : undefined;
}

function commitOf(c: CompareCommit): DeployCommit {
  const title = c.commit.message.split('\n')[0];
  const pr = /\(#(\d+)\)$/.exec(title);
  return { sha: c.sha, title, author: c.commit.author.name, pr: pr ? Number(pr[1]) : undefined, url: c.html_url };
}

function checkState(runs: CheckRun[]): CheckState {
  if (runs.length === 0) return 'none';
  if (runs.some((r) => r.conclusion === 'failure' || r.conclusion === 'timed_out')) return 'failure';
  if (runs.some((r) => r.status !== 'completed')) return 'pending';
  return 'success';
}

async function prInfo(p: Pull): Promise<DeployPRInfo> {
  const [checks, detail] = await Promise.all([
    github<{ check_runs: CheckRun[] }>(`/commits/${p.head.sha}/check-runs?per_page=100`),
    github<PullDetail>(`/pulls/${p.number}`)
  ]);
  return {
    number: p.number,
    title: p.title,
    author: p.user.login,
    url: p.html_url,
    head_sha: p.head.sha,
    draft: p.draft,
    checks: checkState(checks.check_runs),
    codescene: checkState(checks.check_runs.filter((r) => r.name === CODESCENE)),
    mergeable: detail.mergeable === true,
    behind: detail.mergeable_state === 'behind'
  };
}

const SERVICE_DIR = /^app\/(?:db\/|twitch\/|discord\/)?([a-z-]+)\//;

function changedServices(files: { filename: string }[] = []): string[] {
  const out = new Set<string>();
  for (const f of files) {
    const m = SERVICE_DIR.exec(f.filename);
    if (m) out.add(m[1]);
    if (f.filename.startsWith('web/dashboard/')) out.add('console-dashboard');
    if (f.filename.startsWith('web/admin/')) out.add('console-admin');
  }
  return [...out].sort();
}

async function plan(_req: PlanRequest): Promise<DeployPlan> {
  const [main, live, tag, releases, pulls, runs] = await Promise.all([
    github<Branch>(`/branches/${MAIN}`),
    githubMaybe<Ref>(`/git/ref/heads/${DEPLOY_BRANCH}`),
    lastTag(),
    github<Release[]>('/releases?per_page=10'),
    github<Pull[]>(`/pulls?state=open&per_page=${PLAN_PRS}`),
    workflowRuns(10)
  ]);
  const liveSha = live?.object.sha;
  const compare = liveSha ? await github<Compare>(`/compare/${liveSha}...${main.commit.sha}`) : null;
  const active = runs.find((r) => ACTIVE.has(r.status));
  return {
    live_sha: liveSha,
    live_version: releases.find((r) => r.target_commitish === liveSha)?.tag_name,
    last_tag: tag,
    next_version: nextVersion(tag),
    main_sha: main.commit.sha,
    in_sync: liveSha === main.commit.sha,
    commits: (compare?.commits ?? []).map(commitOf).reverse(),
    prs: await Promise.all(pulls.filter((p) => !p.draft).map(prInfo)),
    releases: releases.map((r) => ({ version: r.tag_name, sha: r.target_commitish, url: r.html_url, published_at: r.published_at })),
    services: changedServices(compare?.files),
    active_run_id: active ? String(active.id) : undefined
  };
}

export const releaseApi = { plan, list, get, start, resume, cancel, approve, watch };
