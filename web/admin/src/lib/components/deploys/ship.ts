// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The ship panel's decisions, kept out of the component so the markup only
// renders them: which fields a kind uses, what blocks the button, the one
// line the confirm dialog restates, and the changelog body ?/start receives.
//
// Which fields a kind uses is read off STAGES_FOR rather than listed again
// per kind: a kind that runs no merge_prs stage has no use for ticked PRs,
// and a second table would drift from the deployer's the first time a kind
// gains or loses a stage.

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

/** A release proposes the next version; a hotfix re-ships the current tag. */
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

// A squash merge subject ends in "(#123)"; the changelog links the release,
// not each PR, so the suffix is noise in a highlight.
const PR_SUFFIX = /\s*\(#\d+\)\s*$/;

/** Seed for the English highlights: one commit subject per line. */
export function commitLines(commits: DeployCommit[] | undefined): string {
  return (commits ?? []).map((c) => c.title.replace(PR_SUFFIX, '')).join('\n');
}

const lines = (text: string): string[] =>
  text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean);

/**
 * The ChangelogEntry JSON for the `changelog` field. English is required by
 * the deployer; French is sent only when written, because an empty French
 * block would ship as an empty section instead of falling back to English.
 */
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

// First match wins, so the order is the order an operator fixes things in:
// a run already in flight makes every other field moot. A plan in flight or
// a failed ?/plan blocks too: the confirm line restates the service count,
// and until the picked kind's plan answers that count is the previous
// kind's, which is the wrong number to confirm a production deploy with.
const BLOCKERS: readonly (readonly [(s: ShipState) => boolean, string])[] = [
  [(s) => s.active, 'admin.deploys.runActive'],
  [(s) => s.replanning, 'admin.deploys.planWait'],
  [(s) => s.planFailed, 'admin.deploys.planFailed'],
  [(s) => s.uses.target && !s.targetSha, 'admin.deploys.needPlan'],
  [(s) => s.uses.version && !s.version.trim(), 'admin.deploys.needVersion'],
  [(s) => s.uses.rollback && !s.rollbackTo, 'admin.deploys.needRollback'],
  [(s) => s.uses.changelog && !s.titleEn.trim(), 'admin.deploys.needTitle']
];

/** The i18n key of the first thing stopping a ship, or null when it can go. */
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

/** The version a kind's button and confirm line name. */
export function shipVersion(s: ShipState): string {
  return s.uses.rollback ? s.rollbackTo : s.version.trim();
}

/**
 * Pick the singular key for a count of one: `countKey(1, 'x.commits')` is
 * 'x.commitsOne'. Every counted string here has its `One` twin, the same
 * convention as admin.staff.countOne.
 */
export function countKey(n: number, key: string): string {
  return n === 1 ? `${key}One` : key;
}

/**
 * Params for the confirm line; `sha` is the short target the run tags or
 * pins and `services` the already translated count phrase. The rollback
 * line names no count: ?/plan cannot take the rollback target
 * (deploy.PlanRequest has no field for it), so the plan's services are not
 * the ones that release would roll.
 */
export function confirmParams(s: ShipState, services: string): Record<string, string> {
  return { version: shipVersion(s), sha: shortSha(s.targetSha), services };
}

/**
 * ?/plan for a kind. null on any failure: the panel keeps the plan it has
 * and says the refresh failed, which is safer than blanking the services a
 * ship would roll. The rollback target is not sent: deploy.PlanRequest has
 * no field for it, so the plan cannot depend on it.
 */
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
