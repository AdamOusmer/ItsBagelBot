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
  active: boolean;
  version: string;
  targetSha: string;
  rollbackTo: string;
  titleEn: string;
  replanning: boolean;
  planFailed: boolean;
};

const BLOCKERS: readonly (readonly [(s: ShipState) => boolean, string])[] = [
  [(s) => s.active, 'admin.deploys.runActive'],
  [(s) => s.replanning, 'admin.deploys.planWait'],
  [(s) => s.planFailed, 'admin.deploys.planFailed'],
  [(s) => s.uses.target && !s.targetSha, 'admin.deploys.needPlan'],
  [(s) => s.uses.version && !s.version.trim(), 'admin.deploys.needVersion'],
  [(s) => s.uses.rollback && !s.rollbackTo, 'admin.deploys.needRollback'],
  [(s) => s.uses.changelog && !s.titleEn.trim(), 'admin.deploys.needTitle']
];

export function blocker(s: ShipState): string | null {
  return BLOCKERS.find(([blocked]) => blocked(s))?.[1] ?? null;
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
