// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LaneMutationResult, LanesResult } from '#lib/server/lanes';
import { sampleLanes } from '../fixtures';

export * from '#lib/server/lanes';

const lanes = structuredClone(sampleLanes);

export async function ensureLaneStoreHA(): Promise<void> {}

export async function loadLanes(): Promise<LanesResult> {
  return { lanes: structuredClone(lanes), degraded: false, notice: '' };
}

export async function laneAlias(stream: string, consumer: string, alias: string): Promise<LaneMutationResult> {
  const lane = lanes.find((l) => l.stream === stream && l.consumer === consumer);
  if (!lane) return { ok: false };
  lane.alias = alias;
  lane.display = alias || lane.defaultDisplay || lane.consumer;
  return { ok: true };
}

export async function laneDurable(): Promise<LaneMutationResult> {
  return { ok: true };
}

export async function laneDelete(stream: string, consumer: string): Promise<LaneMutationResult> {
  const at = lanes.findIndex((l) => l.stream === stream && l.consumer === consumer);
  if (at >= 0) lanes.splice(at, 1);
  return { ok: at >= 0 };
}
