// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, fail, redirect, type ActionFailure } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { RpcError } from '@bagel/kit/server/nats';
import type { RpcCode } from '@bagel/kit/server/rpc-code';
import type { Locale } from '@bagel/kit/i18n';
import { requireRole, type AdminIdentity } from './access';
import { audit } from './audit';
import { actionError, badRequest, type ParseResult } from './admin-action';
import {
  deployApprove,
  deployCancel,
  deployGet,
  deployList,
  deployPlan,
  deployResume,
  deployStart,
  watchDeployRun,
  type DeployListener,
  type DeployRunId,
  type DeployRuns
} from './services';
import {
  RUN_KINDS,
  STAGE_IDS,
  type ChangelogEntry,
  type DeployPlan,
  type DeployRun,
  type DeployRunSummary,
  type ListRequest,
  type PlanRequest,
  type RunKind,
  type RunRequest,
  type StartRequest
} from '$lib/deploys/types';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

export interface DeployApi {
  plan(req: PlanRequest): Promise<DeployPlan | null>;
  list(req: ListRequest): Promise<DeployRuns>;
  get(req: RunRequest): Promise<DeployRun>;
  start(req: StartRequest): Promise<DeployRun>;
  resume(req: RunRequest): Promise<DeployRun>;
  cancel(req: RunRequest): Promise<DeployRun>;
  approve(req: RunRequest): Promise<DeployRun>;
  watch(runId: DeployRunId, listener: DeployListener): () => void;
}

const LIVE: DeployApi = {
  plan: deployPlan,
  list: deployList,
  get: deployGet,
  start: deployStart,
  resume: deployResume,
  cancel: deployCancel,
  approve: deployApprove,
  watch: watchDeployRun
};

export async function deployApi(): Promise<DeployApi> {
  if (DEMO) {
    const { demoDeployApi } = await import('./demo-data');
    return demoDeployApi();
  }
  return LIVE;
}

const STATUS_FOR: Record<RpcCode, number> = {
  invalid: 400,
  not_found: 404,
  forbidden: 403,
  conflict: 409,
  unavailable: 503,
  internal: 500
};

export function deployStatus(e: unknown): number {
  if (e instanceof RpcError && e.code) return STATUS_FOR[e.code];
  return 503;
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

type ActionError = ActionFailure<{ error: string }>;

function deployFailure(e: unknown): ActionError {
  return fail(deployStatus(e), { error: message(e) });
}

type Actor = Pick<AdminIdentity, 'id'>;

export type PlanResult = { plan: DeployPlan | null; planError: string | null };

// The plan is streamed: it reads GitHub, the registry and the cluster, and
// awaiting it here held the whole page (and the sidebar click) for seconds.
export type DeploysPageData = {
  planned: Promise<PlanResult>;
  runs: DeployRunSummary[];
  active: DeployRun | null;
  runsError: string | null;
};

type RunsAndActive = { list: DeployRuns; active: DeployRun | null };

const NO_RUNS: RunsAndActive = { list: { runs: [], activeRunId: null }, active: null };

async function runsAndActive(api: DeployApi, actor: Actor): Promise<RunsAndActive> {
  const list = await api.list({ actor_id: actor.id });
  if (!list.activeRunId) return { list, active: null };
  return { list, active: await api.get({ actor_id: actor.id, run_id: list.activeRunId }) };
}

function firstRejection(results: PromiseSettledResult<unknown>[]): string | null {
  const failed = results.find((r): r is PromiseRejectedResult => r.status === 'rejected');
  return failed ? message(failed.reason) : null;
}

async function planResult(api: DeployApi, actor: Actor): Promise<PlanResult> {
  try {
    return { plan: await api.plan({ actor_id: actor.id }), planError: null };
  } catch (e) {
    return { plan: null, planError: message(e) };
  }
}

export async function loadDeploys(actor: Actor): Promise<DeploysPageData> {
  const api = await deployApi();
  const planned = planResult(api, actor);
  const [runs] = await Promise.allSettled([runsAndActive(api, actor)]);
  const listed = runs.status === 'fulfilled' ? runs.value : NO_RUNS;
  return {
    planned,
    runs: listed.list.runs,
    active: listed.active,
    runsError: firstRejection([runs])
  };
}

export async function loadDeployRun(actor: Actor, runId: DeployRunId): Promise<{ run: DeployRun }> {
  const api = await deployApi();
  try {
    return { run: await api.get({ actor_id: actor.id, run_id: runId }) };
  } catch (e) {
    throw error(deployStatus(e), message(e));
  }
}

type FormField = 'kind' | 'version' | 'target_sha' | 'changelog' | 'rollback_to' | 'run_id' | 'stage' | 'rerun';

function field(f: FormData, name: FormField): string {
  return String(f.get(name) ?? '').trim();
}

function optional(f: FormData, name: FormField): string | undefined {
  return field(f, name) || undefined;
}

function member<T extends string>(list: readonly T[], raw: string): T | null {
  return (list as readonly string[]).includes(raw) ? (raw as T) : null;
}

function kindOf(f: FormData): ParseResult<RunKind> {
  const kind = member(RUN_KINDS, field(f, 'kind'));
  return kind ? { value: kind } : badRequest('invalid kind');
}

function planOf(f: FormData): ParseResult<Omit<PlanRequest, 'actor_id'>> {
  const kind = kindOf(f);
  if ('refuse' in kind) return kind;
  return { value: { kind: kind.value, rollback_to: optional(f, 'rollback_to') } };
}

function prsOf(f: FormData): ParseResult<number[]> {
  const prs = f
    .getAll('pr')
    .map((v) => String(v).trim())
    .filter(Boolean)
    .map(Number);
  return prs.every((n) => Number.isInteger(n) && n > 0) ? { value: prs } : badRequest('invalid pr number');
}

function parseJson(raw: string): unknown {
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null;
}

function isChangelog(v: unknown): v is ChangelogEntry {
  if (!isRecord(v)) return false;
  return isRecord(v.title) && isRecord(v.highlights);
}

function changelogOf(f: FormData): ParseResult<ChangelogEntry | undefined> {
  const raw = field(f, 'changelog');
  if (!raw) return { value: undefined };
  const entry = parseJson(raw);
  return isChangelog(entry) ? { value: entry } : badRequest('invalid changelog');
}

type StartFields = Omit<StartRequest, 'actor_id'>;

function startFields(f: FormData): ParseResult<StartFields> {
  const kind = kindOf(f);
  if ('refuse' in kind) return kind;
  const prs = prsOf(f);
  if ('refuse' in prs) return prs;
  const changelog = changelogOf(f);
  if ('refuse' in changelog) return changelog;
  return {
    value: {
      kind: kind.value,
      version: optional(f, 'version'),
      target_sha: optional(f, 'target_sha'),
      prs: prs.value,
      changelog: changelog.value,
      rollback_to: optional(f, 'rollback_to')
    }
  };
}

type RunFields = Omit<RunRequest, 'actor_id'>;

function runFields(f: FormData): ParseResult<RunFields> {
  const runId = field(f, 'run_id');
  if (!runId) return badRequest('run_id required');
  const stageRaw = field(f, 'stage');
  const stage = member(STAGE_IDS, stageRaw);
  if (stageRaw && !stage) return badRequest('invalid stage');
  return { value: { run_id: runId, stage: stage ?? undefined, rerun: field(f, 'rerun') === 'true' || undefined } };
}

type ActionEvent = { request: Request; locals: App.Locals };

type Gated<P> = { admin: AdminIdentity; value: P };

async function gate<P>(event: ActionEvent, parse: (f: FormData) => ParseResult<P>): Promise<Gated<P> | ActionError> {
  const locale: Locale = event.locals.locale;
  const admin = await requireRole(event, 'deploys.manage');
  if (!admin) return fail(403, { error: actionError(locale, 'forbidden') });
  const parsed = parse(await event.request.formData());
  if ('refuse' in parsed) return fail(400, { error: actionError(locale, parsed.message) });
  return { admin, value: parsed.value };
}

type DeployAuditAction = 'deploy_start' | 'deploy_rollback' | 'deploy_resume' | 'deploy_cancel' | 'deploy_approve';

type Recorded = { admin: AdminIdentity; action: DeployAuditAction; target: string; detail: string };

async function recorded(ref: Recorded, call: () => Promise<DeployRun>): Promise<DeployRun> {
  const line = { action: ref.action, detail: ref.detail };
  try {
    const run = await call();
    audit(ref.admin, { ...line, target: run.id, ok: true });
    return run;
  } catch (e) {
    audit(ref.admin, { ...line, target: ref.target, ok: false, error: message(e) });
    throw e;
  }
}

function summary(parts: (string | false | undefined)[]): string {
  return parts.filter(Boolean).join(' ');
}

function startRecord(admin: AdminIdentity, req: StartRequest): Recorded {
  return {
    admin,
    action: req.kind === 'rollback' ? 'deploy_rollback' : 'deploy_start',
    target: summary([req.kind, req.version ?? req.rollback_to]),
    detail: summary([
      req.kind,
      req.version ?? req.rollback_to,
      req.target_sha && `sha ${req.target_sha.slice(0, 12)}`,
      !!req.prs?.length && `prs ${req.prs.join(',')}`
    ])
  };
}

async function planAction(event: ActionEvent) {
  const g = await gate(event, planOf);
  if (!('admin' in g)) return g;
  try {
    return { plan: await (await deployApi()).plan({ actor_id: g.admin.id, ...g.value }) };
  } catch (e) {
    return deployFailure(e);
  }
}

async function startAction(event: ActionEvent) {
  const g = await gate(event, startFields);
  if (!('admin' in g)) return g;
  const req: StartRequest = { actor_id: g.admin.id, ...g.value };
  let run: DeployRun;
  try {
    run = await recorded(startRecord(g.admin, req), async () => (await deployApi()).start(req));
  } catch (e) {
    return deployFailure(e);
  }
  throw redirect(303, `/deploys/${encodeURIComponent(run.id)}`);
}

type RunVerb = 'resume' | 'cancel' | 'approve';

function runAction(verb: RunVerb) {
  return async (event: ActionEvent) => {
    const g = await gate(event, runFields);
    if (!('admin' in g)) return g;
    const req: RunRequest = { actor_id: g.admin.id, ...g.value };
    const ref: Recorded = {
      admin: g.admin,
      action: `deploy_${verb}`,
      target: req.run_id,
      detail: summary([req.stage, req.rerun && 'rerun'])
    };
    try {
      return { run: await recorded(ref, async () => (await deployApi())[verb](req)) };
    } catch (e) {
      return deployFailure(e);
    }
  };
}

export const runActions = {
  cancel: runAction('cancel'),
  resume: runAction('resume'),
  approve: runAction('approve')
};

export const deployActions = {
  plan: planAction,
  start: startAction,
  ...runActions
};

const enc = new TextEncoder();

export class RunRelay {
  private seq = -1;
  private held: DeployRun[] = [];
  private out: ReadableStreamDefaultController<Uint8Array> | null = null;

  offer(run: DeployRun): void {
    if (this.out) this.send(run);
    else this.held.push(run);
  }

  open(out: ReadableStreamDefaultController<Uint8Array>, snapshot: DeployRun): void {
    this.out = out;
    this.send(snapshot);
    for (const run of this.held.splice(0)) this.send(run);
  }

  keepalive(): void {
    this.write(': keepalive\n\n');
  }

  private send(run: DeployRun): void {
    if (run.seq <= this.seq) return;
    this.seq = run.seq;
    this.write(`event: run\ndata: ${JSON.stringify(run)}\n\n`);
  }

  // Writing to a closed controller throws ERR_INVALID_STATE; uncaught in a timer it kills the server.
  private write(chunk: string): void {
    try {
      this.out?.enqueue(enc.encode(chunk));
    } catch {}
  }
}
