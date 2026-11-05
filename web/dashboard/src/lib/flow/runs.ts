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
  if (candidates.length !== 1) return UNNAMED;
  const node = flow.nodes.find((n) => n.id === candidates[0]!.node);
  const spec = node && specFor(node.type);
  return node && spec ? { name: stepLabel(t, spec), line: describe(t, node) } : UNNAMED;
}

export function runStepRows(t: Translate, records: readonly ActionRecord[], flow?: Flow): RunStepRow[] {
  const refs = flow ? flowStepRefs(flow as KitFlow) : [];
  const rows = new Map<string, RunStepRow>();
  for (const record of records) {
    const key = `${record.index}:${record.type}`;
    const row = rows.get(key);
    if (row) {
      row.count += 1;
      row.status = worse(row.status, record.status);
      row.detail = record.status === row.status && record.detail ? record.detail : row.detail;
      continue;
    }
    const candidates = refs.filter((ref) => ref.index === record.index && ref.type === record.type);
    const named = flow ? namedFromGraph(t, flow, candidates) : UNNAMED;
    rows.set(key, { key, index: record.index, type: record.type, status: record.status, count: 1, detail: record.detail ?? '', ...named });
  }
  return [...rows.values()];
}

function knownReason(t: Translate, detail: string): string | null {
  const texts: Record<string, string> = {
    math_error: t('flows.recipe.why.math_error'),
    bad_times: t('flows.recipe.why.bad_times'),
    text_error: t('flows.recipe.why.text_error'),
    reward_not_manageable: t('flows.recipe.why.reward_not_manageable'),
    no_overlay: t('flows.recipe.why.no_overlay'),
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
