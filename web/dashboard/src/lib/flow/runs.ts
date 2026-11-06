// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { flowStepRefs, type ActionRecord, type ActionStatus, type Flow as KitFlow, type FlowStepRef } from '@bagel/kit/automations';
import { stepLabel } from './copy';
import { describe } from './describe';
import type { Translate } from './messages';
import { specFor, type Flow } from './model';

export type RunStepRow = {
  key: string;
  index: number;
  type: string;
  status: ActionStatus;
  count: number;
  detail: string;
  name: string | null;
  line: string | null;
};

const STATUS_RANK: readonly ActionStatus[] = ['ok', 'skipped', 'unsupported', 'limited', 'failed'];

const worse = (a: ActionStatus, b: ActionStatus) => (STATUS_RANK.indexOf(b) > STATUS_RANK.indexOf(a) ? b : a);

type Named = Pick<RunStepRow, 'name' | 'line'>;

const UNNAMED: Named = { name: null, line: null };

function namedFromGraph(t: Translate, flow: Flow, candidates: FlowStepRef[]): Named {
  const nodes = candidates.flatMap((ref) => flow.nodes.find((n) => n.id === ref.node) ?? []);
  const spec = nodes[0] && specFor(nodes[0].type);
  if (!spec) return UNNAMED;
  const lines = [...new Set(nodes.map((node) => describe(t, node)))];
  const line = nodes.length === 1 ? lines[0]! : t('flows.recipe.runOneOf', { steps: lines.join(', ') });
  return { name: nodes.every((node) => node.type === nodes[0]!.type) ? stepLabel(t, spec) : null, line };
}

// A record names only its top-level action and type. Steps on one arm always run in drawn order, repeating under a loop, so
// the nth record is known. Across arms any of them may have run, and parallel arms interleave, so those stay a list.
function placedAmong(candidates: FlowStepRef[], nth: number): FlowStepRef[] {
  if (candidates.length < 2 || candidates.some((ref) => ref.arm !== candidates[0]!.arm)) return candidates;
  return [candidates[nth % candidates.length]!];
}

function tally(row: RunStepRow, record: ActionRecord): void {
  row.count += 1;
  row.status = worse(row.status, record.status);
  row.detail = record.status === row.status && record.detail ? record.detail : row.detail;
}

export function runStepRows(t: Translate, records: readonly ActionRecord[], flow?: Flow): RunStepRow[] {
  const refs = flow ? flowStepRefs(flow as KitFlow) : [];
  const rows = new Map<string, RunStepRow>();
  const seen = new Map<string, number>();
  for (const record of records) {
    const group = `${record.index}:${record.type}`;
    const nth = seen.get(group) ?? 0;
    seen.set(group, nth + 1);
    const placed = placedAmong(refs.filter((ref) => ref.index === record.index && ref.type === record.type), nth);
    const key = placed.length === 1 ? `${group}:${placed[0]!.node}` : group;
    const row = rows.get(key);
    if (row) tally(row, record);
    else rows.set(key, { key, index: record.index, type: record.type, status: record.status, count: 1, detail: record.detail ?? '', ...(flow ? namedFromGraph(t, flow, placed) : UNNAMED) });
  }
  return [...rows.values()];
}

function knownReason(t: Translate, detail: string): string | null {
  const texts: Record<string, string> = {
    math_error: t('flows.recipe.why.math_error'),
    bad_times: t('flows.recipe.why.bad_times'),
    text_error: t('flows.recipe.why.text_error'),
    reward_not_manageable: t('flows.recipe.why.reward_not_manageable'),
    reward_not_found: t('flows.recipe.why.reward_not_found'),
    reward_rejected: t('flows.recipe.why.reward_rejected'),
    reward_unconfirmed: t('flows.recipe.why.reward_unconfirmed'),
    queue_full: t('flows.recipe.why.queue_full'),
    queue_timeout: t('flows.recipe.why.queue_timeout'),
    queued: t('flows.recipe.why.queued')
  };
  return Object.hasOwn(texts, detail) ? texts[detail]! : null;
}

function byStatus(t: Translate, status: ActionStatus, detail: string): string {
  if (status === 'limited') return t('flows.recipe.why.limited');
  if (status === 'unsupported') return t('flows.recipe.why.unsupported');
  if (status === 'skipped') return t('flows.recipe.why.skipped');
  return detail ? t('flows.recipe.why.failedWith', { detail }) : t('flows.recipe.why.failed');
}

export function explainStep(t: Translate, status: ActionStatus, detail: string): string {
  const known = knownReason(t, detail);
  if (known) return known;
  return status === 'ok' ? detail : byStatus(t, status, detail);
}

export function explainRun(t: Translate, reason: string | undefined): string {
  if (!reason) return '';
  if (reason.startsWith('condition:')) return t('flows.recipe.runStopped');
  return knownReason(t, reason) ?? reason;
}
