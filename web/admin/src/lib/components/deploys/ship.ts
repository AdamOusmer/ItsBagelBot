// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { deserialize } from '$app/forms';
import type { ChangelogEntry, DeployCommit, DeployPlan, RunKind } from '$lib/deploys/types';
import { STAGES_FOR } from '$lib/deploys/types';
import { shortSha } from './view';

export type KindUses = {
  prs: boolean;
  changelog: boolean;
  target: boolean;
  version: boolean;
  rollback: boolean;
};

export function usesFor(kind: RunKind): KindUses {
  const stages = STAGES_FOR[kind];
  return {
    prs: stages.includes('merge_prs'),
    changelog: stages.includes('changelog'),
    target: stages.includes('build'),
    version: stages.includes('tag'),
    rollback: kind === 'rollback'
  };
}

export function defaultVersion(kind: RunKind, plan: DeployPlan | null): string {
  if (kind === 'release') return plan?.next_version ?? '';
  if (kind === 'hotfix') return plan?.last_tag ?? '';
  return '';
}

export type ChangelogDraft = {
  titleEn: string;
  titleFr: string;
  highlightsEn: string;
  highlightsFr: string;
};

const PR_SUFFIX = /\s*\(#\d+\)\s*$/;

export function commitLines(commits: DeployCommit[] | undefined): string {
  return (commits ?? []).map((c) => c.title.replace(PR_SUFFIX, '')).join('\n');
}

const lines = (text: string): string[] =>
  text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean);

export function changelogJSON(d: ChangelogDraft): string {
  const entry: ChangelogEntry = {
    title: { en: d.titleEn.trim() },
    highlights: { en: lines(d.highlightsEn) }
  };
  const titleFr = d.titleFr.trim();
  const highlightsFr = lines(d.highlightsFr);
  if (titleFr) entry.title.fr = titleFr;
  if (highlightsFr.length > 0) entry.highlights.fr = highlightsFr;
  return JSON.stringify(entry);
}

export type ShipState = {
  kind: RunKind;
  uses: KindUses;
  version: string;
  targetSha: string;
  rollbackTo: string;
  titleEn: string;
  replanning: boolean;
  planFailed: boolean;
};

export type ShipStep = 'kind' | 'build' | 'rollback' | 'prs' | 'changelog' | 'review';

const STEP_USE: readonly (readonly [ShipStep, (u: KindUses) => boolean])[] = [
  ['kind', () => true],
  ['build', (u) => u.target],
  ['rollback', (u) => u.rollback],
  ['prs', (u) => u.prs],
  ['changelog', (u) => u.changelog],
  ['review', () => true]
];

export function stepsFor(uses: KindUses): ShipStep[] {
  return STEP_USE.filter(([, on]) => on(uses)).map(([step]) => step);
}

type Blocker = readonly [ShipStep | 'any', (s: ShipState) => boolean, string];

const BLOCKERS: readonly Blocker[] = [
  ['any', (s) => s.replanning, 'admin.deploys.planWait'],
  ['any', (s) => s.planFailed, 'admin.deploys.planFailed'],
  ['build', (s) => s.uses.target && !s.targetSha, 'admin.deploys.needPlan'],
  ['build', (s) => s.uses.version && !s.version.trim(), 'admin.deploys.needVersion'],
  ['rollback', (s) => s.uses.rollback && !s.rollbackTo, 'admin.deploys.needRollback'],
  ['changelog', (s) => s.uses.changelog && !s.titleEn.trim(), 'admin.deploys.needTitle']
];

export function blocker(s: ShipState): string | null {
  return BLOCKERS.find(([, blocked]) => blocked(s))?.[2] ?? null;
}

export function stepBlocker(s: ShipState, step: ShipStep): string | null {
  if (step === 'review') return blocker(s);
  return BLOCKERS.find(([at, blocked]) => (at === step || at === 'any') && blocked(s))?.[2] ?? null;
}

export function reachable(s: ShipState, steps: ShipStep[], furthest: number): number {
  const last = steps.length - 1;
  const stuck = steps.findIndex((step, i) => i < last && stepBlocker(s, step) !== null);
  return Math.min(furthest + 1, stuck === -1 ? last : stuck);
}

const SHIP_KEY: Record<RunKind, string> = {
  release: 'admin.deploys.ship',
  hotfix: 'admin.deploys.ship',
  bump: 'admin.deploys.shipBump',
  rollback: 'admin.deploys.shipRollback',
  reapply: 'admin.deploys.shipReapply'
};

export function shipKey(kind: RunKind): string {
  return SHIP_KEY[kind];
}

export function shipVersion(s: ShipState): string {
  return s.uses.rollback ? s.rollbackTo : s.version.trim();
}

export function countKey(n: number, key: string): string {
  return n === 1 ? `${key}One` : key;
}

export function confirmParams(s: ShipState, services: string): Record<string, string> {
  return { version: shipVersion(s), sha: shortSha(s.targetSha), services };
}

export async function fetchPlan(kind: RunKind, rollbackTo = ''): Promise<DeployPlan | null> {
  const body = new FormData();
  body.set('kind', kind);
  if (rollbackTo) body.set('rollback_to', rollbackTo);
  try {
    const res = await fetch('/deploys?/plan', { method: 'POST', body });
    const result = deserialize(await res.text());
    if (result.type !== 'success') return null;
    return (result.data as { plan?: DeployPlan | null } | undefined)?.plan ?? null;
  } catch {
    return null;
  }
}

export const STEP_RAIL_KEY: Record<ShipStep, string> = {
  kind: 'admin.deploys.flow.rail.kind',
  build: 'admin.deploys.flow.rail.build',
  rollback: 'admin.deploys.flow.rail.rollback',
  prs: 'admin.deploys.flow.rail.prs',
  changelog: 'admin.deploys.flow.rail.changelog',
  review: 'admin.deploys.flow.rail.review'
};

export const STEP_TITLE_KEY: Record<ShipStep, string> = {
  kind: 'admin.deploys.flow.title.kind',
  build: 'admin.deploys.flow.title.build',
  rollback: 'admin.deploys.flow.title.rollback',
  prs: 'admin.deploys.flow.title.prs',
  changelog: 'admin.deploys.flow.title.changelog',
  review: 'admin.deploys.flow.title.review'
};

export const STEP_BODY_KEY: Record<ShipStep, string> = {
  kind: 'admin.deploys.flow.body.kind',
  build: 'admin.deploys.flow.body.build',
  rollback: 'admin.deploys.flow.body.rollback',
  prs: 'admin.deploys.flow.body.prs',
  changelog: 'admin.deploys.flow.body.changelog',
  review: 'admin.deploys.flow.body.review'
};
