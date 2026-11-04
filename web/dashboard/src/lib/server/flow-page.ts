// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  ALERT_KEYS,
  MAX_PREVIEW_BYTES,
  definitionToFlow,
  flowToDefinition,
  locateIssues,
  validateDefinition,
  type Definition,
  type Flow,
  type FlowConfig,
  type Issue
} from '@bagel/kit/automations';
import { MOD, moduleDef } from '@bagel/kit/catalog';
import { cardFromSummary, type FlowCard } from '#lib/flow/cards';
import { newFlow } from '#lib/flow/model';
import { NEW_FLOW_ID, flowRouteId, parseFlowRouteId, type FlowRef } from '#lib/flow/ref';
import type { DeleteReply, SaveReply, SaveRequest, ToggleReply } from '#lib/flow/replies';
import { flowPreview } from '#lib/flow/thumb';
import { effectiveId } from './board';
import { attachChoiceNames } from './flow-choices';
import { deleteFlow, listFlowSummaries, loadFlow, saveFlow, setFlowEnabled, type SaveFlowResult } from './flow-store';
import { moduleWriteDenial } from './module-gate';
import { attachLogins } from './user-login';

const MAX_PAYLOAD_BYTES = 256 * 1024;
const MAX_NODES = 200;
const MAX_EDGES = 400;
const MAX_TEXT = 4096;

export type EditorData = { routeId: string; ref: FlowRef | null; revision: string | null; name: string; flow: Flow | null };

export type Lowered = { request: SaveRequest; flow: Flow; definition: Definition; preview: string | undefined };

export async function flowDenial(locals: App.Locals): Promise<{ ok: false; reason: 'denied'; error: string } | null> {
  const denied = await moduleWriteDenial(locals, moduleDef(MOD.automations)!);
  return denied ? { ok: false, reason: 'denied', error: denied.data.error } : null;
}

export async function liveCards(locals: App.Locals): Promise<FlowCard[]> {
  return (await listFlowSummaries(effectiveId(locals.session))).map(cardFromSummary);
}

const isRecord = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const isText = (value: unknown, max = MAX_TEXT): value is string => typeof value === 'string' && value.length <= max;
const isPrimitive = (value: unknown) => ['string', 'number', 'boolean'].includes(typeof value);

function configOf(value: unknown): FlowConfig | null {
  if (!isRecord(value) || !Object.values(value).every(isPrimitive)) return null;
  return value as FlowConfig;
}

function nodeOf(value: unknown): Flow['nodes'][number] | null {
  if (!isRecord(value)) return null;
  const config = configOf(value.config);
  if (!isText(value.id, 64) || !isText(value.type, 64) || !config) return null;
  return { id: value.id, type: value.type, x: Number(value.x) || 0, y: Number(value.y) || 0, config };
}

function edgeOf(value: unknown): Flow['edges'][number] | null {
  if (!isRecord(value)) return null;
  const { id, from, port, to } = value;
  return isText(id, 64) && isText(from, 64) && isText(port, 64) && isText(to, 64) ? { id, from, port, to } : null;
}

function listOf<T>(value: unknown, max: number, read: (item: unknown) => T | null): T[] | null {
  if (!Array.isArray(value) || value.length > max) return null;
  const items = value.map(read);
  return items.every((item) => item !== null) ? (items as T[]) : null;
}

const refReaders = new Map<unknown, (value: Record<string, unknown>) => FlowRef | undefined>([
  ['automation', (value) => (isText(value.id, 128) ? { layer: 'automation', id: value.id } : undefined)],
  ['command', (value) => (isText(value.name, 128) ? { layer: 'command', name: value.name } : undefined)],
  ['alert', (value) => {
    const alert = ALERT_KEYS.find((key) => key === value.alert);
    return alert ? { layer: 'alert', alert } : undefined;
  }]
]);

function refOf(value: unknown): FlowRef | null | undefined {
  if (value === null) return null;
  return isRecord(value) ? refReaders.get(value.layer)?.(value) : undefined;
}

function parseJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function flowOf(value: unknown): Flow | null {
  if (!isRecord(value) || !isText(value.name, 200)) return null;
  const nodes = listOf(value.nodes, MAX_NODES, nodeOf);
  const edges = listOf(value.edges, MAX_EDGES, edgeOf);
  if (!nodes || !edges) return null;
  return { id: NEW_FLOW_ID, name: value.name.trim(), enabled: value.enabled === true, nodes, edges };
}

export function parseSaveRequest(text: string): { request: SaveRequest; flow: Flow } | null {
  const body = text.length > MAX_PAYLOAD_BYTES ? undefined : parseJson(text);
  if (!isRecord(body)) return null;
  const flow = flowOf(body.flow);
  const ref = refOf(body.ref);
  if (!flow || ref === undefined) return null;
  const expected = typeof body.expectedRevision === 'string' ? body.expectedRevision : undefined;
  return { flow, request: { flow, ref, expectedRevision: expected, confirmUpgrade: body.confirmUpgrade === true } };
}

function previewOf(flow: Flow): string | undefined {
  const text = JSON.stringify(flowPreview(flow));
  return new TextEncoder().encode(text).length <= MAX_PREVIEW_BYTES ? text : undefined;
}

const invalid = (issues: Issue[]): { ok: false; reply: SaveReply } => ({ ok: false, reply: { ok: false, reason: 'invalid', issues } });

export function lowerSaveRequest(text: string): { ok: true; lowered: Lowered } | { ok: false; reply: SaveReply } {
  const parsed = parseSaveRequest(text);
  if (!parsed) return invalid([{ path: '', code: 'invalid_format' }]);
  const lowered = flowToDefinition(parsed.flow);
  if (!lowered.ok) return invalid(lowered.issues);
  const issues = validateDefinition(lowered.value);
  if (issues.length > 0) return invalid(locateIssues(parsed.flow, issues));
  return { ok: true, lowered: { ...parsed, definition: lowered.value, preview: previewOf(parsed.flow) } };
}

export function saveReply(result: SaveFlowResult): SaveReply {
  if (result.ok) {
    const { flow } = result;
    return { ok: true, id: flowRouteId(flow.ref), name: flow.name, enabled: flow.enabled, revision: flow.revision, warnings: result.warnings, upgrade: result.upgrade };
  }
  switch (result.reason) {
    case 'invalid':
    case 'conflict':
      return { ok: false, reason: result.reason, issues: result.issues };
    case 'needs_upgrade':
      return { ok: false, reason: 'needs_upgrade', upgrade: result.upgrade };
    case 'denied':
      return { ok: false, reason: 'denied', error: result.error };
    case 'failed':
      return { ok: false, reason: 'failed' };
    default:
      return { ok: false, reason: 'not_found' };
  }
}

const isStale = (reply: SaveReply): reply is Extract<SaveReply, { ok: false }> =>
  !reply.ok && reply.reason === 'conflict' && (reply.issues ?? []).length === 0;

async function withLatestRevision(locals: App.Locals, ref: FlowRef | null, reply: SaveReply): Promise<SaveReply> {
  if (!ref || !isStale(reply)) return reply;
  const latest = await loadFlow(effectiveId(locals.session), ref).catch(() => null);
  return { ...reply, latestRevision: latest?.revision };
}

export async function liveSave(locals: App.Locals, { request, flow, definition, preview }: Lowered): Promise<SaveReply> {
  const result = await saveFlow(locals, {
    ref: request.ref,
    name: flow.name,
    enabled: flow.enabled,
    definition,
    preview,
    expectedRevision: request.expectedRevision,
    confirmUpgrade: request.confirmUpgrade
  });
  return withLatestRevision(locals, request.ref, saveReply(result));
}

export async function liveToggle(locals: App.Locals, ref: FlowRef, enabled: boolean, revision?: string): Promise<ToggleReply> {
  const result = await setFlowEnabled(locals, ref, enabled, revision);
  if (result.ok) return { ok: true, enabled: result.enabled, revision: result.revision, warnings: result.warnings };
  return result.reason === 'denied' ? { ok: false, reason: 'denied', error: result.error } : { ok: false, reason: result.reason };
}

export async function liveDelete(locals: App.Locals, ref: FlowRef): Promise<DeleteReply> {
  const result = await deleteFlow(locals, ref);
  if (result.ok) return { ok: true };
  return result.reason === 'denied' ? { ok: false, reason: 'denied', error: result.error } : { ok: false, reason: result.reason };
}

export const newEditor = (): EditorData => ({ routeId: NEW_FLOW_ID, ref: null, revision: null, name: '', flow: newFlow('') });

export async function liveEditor(locals: App.Locals, routeId: string): Promise<EditorData | null> {
  if (routeId === NEW_FLOW_ID) return newEditor();
  const ref = parseFlowRouteId(routeId);
  const stored = ref ? await loadFlow(effectiveId(locals.session), ref) : null;
  if (!ref || !stored) return null;
  const drawn = definitionToFlow(stored.definition, { id: routeId, name: stored.name, enabled: stored.enabled });
  if (drawn.ok) await Promise.all([attachLogins(drawn.value.nodes), attachChoiceNames(effectiveId(locals.session), drawn.value.nodes)]);
  return { routeId, ref, revision: stored.revision, name: stored.name, flow: drawn.ok ? drawn.value : null };
}
