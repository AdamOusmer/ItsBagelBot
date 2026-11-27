// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DeployPlanResult, DeployRunsResult, FluxResult, ListRequest } from '#lib/deploys/types';
import type { DeployApi } from './deploys';
import { fluxTiers, PINNABLE } from './flux';

type Actor = Pick<ListRequest, 'actor_id'>;

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export async function readDeployRuns(api: Promise<DeployApi>, actor: Actor): Promise<DeployRunsResult> {
  try {
    const release = await api;
    const { runs, activeRunId } = await release.list(actor);
    const active = activeRunId ? await release.get({ ...actor, run_id: activeRunId }) : null;
    return { runs, active, runsError: null };
  } catch (error) {
    return { runs: [], active: null, runsError: message(error) };
  }
}

export async function readDeployPlan(api: Promise<DeployApi>, actor: Actor): Promise<DeployPlanResult> {
  try {
    const release = await api;
    return { plan: await release.plan(actor), planError: null };
  } catch (error) {
    return { plan: null, planError: message(error) };
  }
}

export async function readDeployFlux(): Promise<FluxResult> {
  try {
    return { tiers: await fluxTiers(), pinnable: PINNABLE, fluxError: null };
  } catch (error) {
    return { tiers: [], pinnable: PINNABLE, fluxError: message(error) };
  }
}
