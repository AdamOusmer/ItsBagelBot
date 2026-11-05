// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { fail, type ActionFailure } from '@sveltejs/kit';
import {
  commitFlowImport,
  definitionToFlow,
  exportFlows,
  flowImportView,
  flowResultView,
  MAX_FLOWS_PER_FILE,
  MAX_FLOW_FILE_BYTES,
  MAX_PREVIEW_BYTES,
  needsSetup,
  planFlowImport,
  serializeFlowFile,
  type Definition,
  type Flow,
  type FlowExportRow,
  type FlowImportContext,
  type FlowImportEntry,
  type FlowWrite,
  type FlowWriteResult,
  type Issue
} from '@bagel/kit/automations';
import { moduleDef, MOD } from '@bagel/kit/catalog';
import { logger } from '@bagel/kit/server/logger';
import { ValkeyRateLimiter } from '@bagel/kit/server/rate-limit';
import { flowKey } from '#lib/flow-share';
import { actionError } from './action-errors';
import { effectiveId } from './board';
import { listCommands } from './commands-store';
import { listFlows, saveFlow, takenAlerts, type SaveFlowResult, type StoredFlow } from './flow-store';
import { moduleWriteDenial } from './module-gate';
import { auditDashboardImpersonation } from './services';

const DEMO = dev && process.env.DEMO === '1';

const AUDIT_NAME = 'automations';
const RATE_LIMIT_ERROR = 'Too many import attempts. Wait a minute and try again.';
const UNAVAILABLE_ERROR = 'Could not update. Try again in a moment.';
const TOO_LARGE_ERROR = 'That recipe file is too large.';

const exportLimiter = new ValkeyRateLimiter({ name: 'flowexport', capacity: 10, refillPerSec: 10 / 60 });
const importLimiter = new ValkeyRateLimiter({ name: 'flowimport', capacity: 10, refillPerSec: 10 / 60 });

export interface FlowShareOptions {
  flowPreview: (flow: Flow) => unknown;
  demoFlows?: () => Promise<FlowExportRow[]>;
}

type ShareEvent = { request: Request; locals: App.Locals };
type Failure = ActionFailure<Record<string, unknown>>;

async function shareDenial(locals: App.Locals, limiter: ValkeyRateLimiter): Promise<Failure | null> {
  if (!DEMO && !locals.session) return fail(401, { ok: false, error: actionError(locals.locale, 'Not signed in.') });
  const denied = await moduleWriteDenial(locals, moduleDef(MOD.automations)!);
  if (denied) return denied;
  if (DEMO) return null;
  const decision = await limiter.check(`${AUDIT_NAME}:${effectiveId(locals.session)}`);
  return decision.allowed ? null : fail(429, { ok: false, code: 'rate_limited', error: actionError(locals.locale, RATE_LIMIT_ERROR) });
}

export function regeneratedPreview(flowPreview: FlowShareOptions['flowPreview'], definition: Definition, name: string): string | undefined {
  const flow = definitionToFlow(definition, { id: 'import', name, enabled: false });
  if (!flow.ok) return undefined;
  const text = JSON.stringify(flowPreview(flow.value));
  return new TextEncoder().encode(text).length <= MAX_PREVIEW_BYTES ? text : undefined;
}

function commandTriggerNames(definition: Definition): string[] {
  if (definition.trigger.type !== 'command') return [];
  const params = definition.trigger.params ?? {};
  return [params.name, ...((params.aliases ?? []) as unknown[])].map((name) => String(name).replace(/^!/, ''));
}

const isAutomation = (flow: StoredFlow) => flow.ref.layer === 'automation';

async function importContext(uid: string): Promise<FlowImportContext> {
  if (DEMO) return { count: 0, names: [], commands: [], alerts: [] };
  const [flows, commands, alerts] = await Promise.all([listFlows(uid), listCommands(uid), takenAlerts(uid)]);
  const automations = flows.filter(isAutomation);
  return {
    count: automations.length,
    names: automations.map((row) => row.name),
    commands: [...commands.flatMap((row) => [row.name, ...(row.aliases ?? [])]), ...automations.flatMap((row) => commandTriggerNames(row.definition))],
    alerts
  };
}

function writeRefusal(result: Exclude<SaveFlowResult, { ok: true }>): Issue[] {
  if ('issues' in result && result.issues.length > 0) return result.issues;
  return [{ path: '', code: result.reason === 'conflict' ? 'duplicate' : 'write_failed' }];
}

async function writeFlow(locals: App.Locals, input: FlowWrite): Promise<FlowWriteResult> {
  if (DEMO) return { ok: true, id: `demo-${input.name}`, revision: 1 };
  const result = await saveFlow(locals, {
    ref: input.id ? { layer: 'automation', id: input.id } : null,
    name: input.name,
    enabled: false,
    definition: input.definition,
    preview: input.preview,
    expectedRevision: input.expectedRevision === undefined ? undefined : String(input.expectedRevision)
  });
  if (!result.ok) return { ok: false, issues: writeRefusal(result) };
  return result.flow.ref.layer === 'automation' ? { ok: true, id: result.flow.ref.id, revision: Number(result.flow.revision) } : { ok: true };
}

function rowOf(flow: StoredFlow): FlowExportRow {
  return { id: flow.ref.layer === 'automation' ? flow.ref.id : undefined, name: flow.name, definition: flow.definition, restricted: flow.restricted };
}

async function ownedRows(uid: string, keys: Set<string>, options: FlowShareOptions): Promise<FlowExportRow[]> {
  if (DEMO) return (await options.demoFlows?.())?.filter((row) => keys.has(`automation:${row.id}`)) ?? [];
  const flows = await listFlows(uid);
  return flows.filter((flow) => keys.has(flowKey(flow.ref))).map(rowOf);
}

function rpcFailure(locals: App.Locals, op: string, err: unknown): Failure {
  logger.error({ err }, `[flow-share] ${op} failed`);
  return fail(400, { ok: false, error: actionError(locals.locale, UNAVAILABLE_ERROR) });
}

async function exportAction({ request, locals }: ShareEvent, options: FlowShareOptions) {
  const denied = await shareDenial(locals, exportLimiter);
  if (denied) return denied;
  const form = await request.formData();
  const keys = new Set(form.getAll('flow').map(String));
  if (keys.size === 0 || keys.size > MAX_FLOWS_PER_FILE) return fail(400, { ok: false, error: actionError(locals.locale, 'Invalid input.') });
  const uid = effectiveId(locals.session);
  try {
    const rows = await ownedRows(uid, keys, options);
    const { file, report } = exportFlows(rows);
    auditDashboardImpersonation(locals.session ?? null, `${AUDIT_NAME}:flow_export`, String(file.flows.length));
    return {
      ok: true,
      file: serializeFlowFile(file),
      exported: file.flows.length,
      needSetup: report.filter((row) => row.setup.some(needsSetup)).length,
      missing: keys.size - rows.length,
      scrubbed: report.filter((row) => row.scrubbed.length > 0).map((row) => ({ name: row.name, notes: row.scrubbed })),
      failed: report.filter((row) => row.issues.length > 0).map((row) => row.name)
    };
  } catch (err) {
    return rpcFailure(locals, 'export', err);
  }
}

type FormFile = { text?: string; refusal?: Failure };

async function readFile(form: FormData, locals: App.Locals): Promise<FormFile> {
  const file = form.get('file');
  if (!(file instanceof File) || file.size === 0) return { refusal: fail(400, { ok: false, error: actionError(locals.locale, 'Could not read that file.') }) };
  if (file.size > MAX_FLOW_FILE_BYTES) return { refusal: fail(413, { ok: false, code: 'file_too_large', error: actionError(locals.locale, TOO_LARGE_ERROR) }) };
  return { text: await file.text() };
}

function selection(form: FormData): number[] {
  return form
    .getAll('index')
    .map((value) => Number(value))
    .filter((index) => Number.isInteger(index) && index >= 0 && index < MAX_FLOWS_PER_FILE);
}

const fileRefusal = (issues: { path: string; code: string }[]) => fail(400, { ok: false, code: 'invalid_file', issues });

async function previewAction({ request, locals }: ShareEvent) {
  const denied = await shareDenial(locals, importLimiter);
  if (denied) return denied;
  const { text, refusal } = await readFile(await request.formData(), locals);
  if (refusal) return refusal;
  try {
    const plan = planFlowImport(text!, await importContext(effectiveId(locals.session)));
    return plan.ok ? { ok: true, entries: plan.entries.map(flowImportView), slots: plan.slots } : fileRefusal(plan.issues);
  } catch (err) {
    return rpcFailure(locals, 'preview', err);
  }
}

async function commitPlan(locals: App.Locals, entries: FlowImportEntry[], options: FlowShareOptions) {
  return commitFlowImport(entries, {
    write: (input) => writeFlow(locals, input),
    preview: (definition, name) => regeneratedPreview(options.flowPreview, definition, name)
  });
}

async function commitAction({ request, locals }: ShareEvent, options: FlowShareOptions) {
  const denied = await shareDenial(locals, importLimiter);
  if (denied) return denied;
  const form = await request.formData();
  const { text, refusal } = await readFile(form, locals);
  if (refusal) return refusal;
  const selected = selection(form);
  if (selected.length === 0) return fail(400, { ok: false, error: actionError(locals.locale, 'Nothing selected to import.') });
  const uid = effectiveId(locals.session);
  try {
    const plan = planFlowImport(text!, await importContext(uid), selected);
    if (!plan.ok) return fileRefusal(plan.issues);
    const results = await commitPlan(locals, plan.entries, options);
    const created = results.filter((result) => result.status === 'created').length;
    auditDashboardImpersonation(locals.session ?? null, `${AUDIT_NAME}:flow_import`, String(created));
    return { ok: true, results: results.map(flowResultView) };
  } catch (err) {
    return rpcFailure(locals, 'commit', err);
  }
}

export function flowShareActions(options: FlowShareOptions) {
  return {
    flowExport: (event: ShareEvent) => exportAction(event, options),
    flowImportPreview: (event: ShareEvent) => previewAction(event),
    flowImportCommit: (event: ShareEvent) => commitAction(event, options)
  };
}
