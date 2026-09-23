// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Deploys page's server half, shared by /deploys and /deploys/[id]: the
// owner gate, form parsing, the audit trail, the SSE frame ordering and the
// page's one DEMO switch.
//
// Owner-only at three layers, and this is the first: ROLE_FOR['deploys.manage']
// on every load, action and the stream. The second is the NATS account (only
// ADMIN_RPC may call bagel.rpc.admin.deploy.>). The third is the deployer,
// which reads the actor's role from the users service on every verb, so a
// console bug here still cannot start a run for an admin.
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

// Build-time constant, branched on directly: see access.ts for why this is
// process.env and not $env/dynamic/private.
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

// The one DEMO switch for the whole page. The fixture API has the live one's
// shape, so the loads, the actions and the stream run the same code in both
// modes and no route file carries a branch of its own.
export async function deployApi(): Promise<DeployApi> {
  if (DEMO) {
    const { demoDeployApi } = await import('./demo-data');
    return demoDeployApi();
  }
  return LIVE;
}

// ── Refusals ────────────────────────────────────────────────────────────────

// The deployer's refusal code picks the HTTP status, so the page can tell "you
// may not" (403) from "a run is already active" (409) from "that run id is not
// among the kept ones" (404). An error that is not an RpcError never reached a
// deployer (no responder, timeout): 503.
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

// ── Loads ───────────────────────────────────────────────────────────────────

type Actor = Pick<AdminIdentity, 'id'>;

export type DeploysPageData = {
  plan: DeployPlan | null;
  runs: DeployRunSummary[];
  active: DeployRun | null;
  planError: string | null;
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

// The plan and the run history are read in parallel and fail apart: a plan
// that times out on GitHub still renders the history and the active run, with
// the reason in planError. A failed history read lands in planError too; both
// come from the same deployer and the page has one place to say it is down.
export async function loadDeploys(actor: Actor): Promise<DeploysPageData> {
  const api = await deployApi();
  const [plan, runs] = await Promise.allSettled([api.plan({ actor_id: actor.id }), runsAndActive(api, actor)]);
  const listed = runs.status === 'fulfilled' ? runs.value : NO_RUNS;
  return {
    plan: plan.status === 'fulfilled' ? plan.value : null,
    runs: listed.list.runs,
    active: listed.active,
    planError: firstRejection([plan, runs])
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

// ── Form parsing ────────────────────────────────────────────────────────────

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

// The plan's kind plus, for a rollback, the release it would pin.
function planOf(f: FormData): ParseResult<Omit<PlanRequest, 'actor_id'>> {
  const kind = kindOf(f);
  if ('refuse' in kind) return kind;
  return { value: { kind: kind.value, rollback_to: optional(f, 'rollback_to') } };
}

// Repeated `pr` fields, one PR number each.
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

// The changelog is one JSON field holding a ChangelogEntry, because its title
// and highlights are per-locale maps that do not flatten into form fields. The
// deployer owns the file (it adds tag, version and the GitHub URL, and fixes
// the key order); this refuses only what is not an entry at all.
function changelogOf(f: FormData): ParseResult<ChangelogEntry | undefined> {
  const raw = field(f, 'changelog');
  if (!raw) return { value: undefined };
  const entry = parseJson(raw);
  return isChangelog(entry) ? { value: entry } : badRequest('invalid changelog');
}

type StartFields = Omit<StartRequest, 'actor_id'>;

// Whether a field is required for a kind (version for a release, rollback_to
// for a rollback) is the deployer's call, not repeated here: it refuses with
// `invalid`, which reaches the page as a 400 like the refusals below.
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

// resume, cancel and approve address one run. `stage` and `rerun` are the
// optional RunRequest refinements: rerun=true is how the page asks for a
// failure's rerun_failed_jobs action through resume.
function runFields(f: FormData): ParseResult<RunFields> {
  const runId = field(f, 'run_id');
  if (!runId) return badRequest('run_id required');
  const stageRaw = field(f, 'stage');
  const stage = member(STAGE_IDS, stageRaw);
  if (stageRaw && !stage) return badRequest('invalid stage');
  return { value: { run_id: runId, stage: stage ?? undefined, rerun: field(f, 'rerun') === 'true' || undefined } };
}

// ── Actions ─────────────────────────────────────────────────────────────────

type ActionEvent = { request: Request; locals: App.Locals };

type Gated<P> = { admin: AdminIdentity; value: P };

// gate is the spine every action here shares: owner check, then the form
// parse. Every parse refusal is a bad request, since each one names a field
// the page's own controls always send well-formed.
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

// recorded runs one deployer write and writes its audit row either way. On
// success the row's target is the run id, so a start's row names the run it
// created; a refused start has no run, and keeps the kind and version.
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

// A rollback is its own audit action rather than a start with kind=rollback in
// the detail: it is the one start that moves the cluster to older images, and
// the trail's filter should find it without parsing details.
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

// The redirect is thrown outside the try: SvelteKit's redirect is itself a
// throw, and inside the try it would be caught and answered as a failure.
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

/** The run-scoped actions, on both /deploys and /deploys/[id]. */
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

// ── Stream ──────────────────────────────────────────────────────────────────

const enc = new TextEncoder();

// RunRelay orders one SSE stream's frames: the snapshot first, then only
// snapshots with a higher seq. The stream watches before it reads the
// snapshot, so a frame published between the two is held here and sent after
// the snapshot if it is newer; the other order would lose it, and if that
// frame was the terminal one the page would show a run still in progress.
// Anything at or below the seq already sent is skipped, so a gap re-read or a
// late duplicate cannot move the page backwards.
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

  // Writes land from timers and NATS callbacks, after the client may have
  // gone. Enqueueing on a closed controller throws ERR_INVALID_STATE, and
  // uncaught in a timer that takes the whole server down (the events stream
  // did exactly that in dev), so a closed stream swallows the write.
  private write(chunk: string): void {
    try {
      this.out?.enqueue(enc.encode(chunk));
    } catch {
      /* closed */
    }
  }
}
