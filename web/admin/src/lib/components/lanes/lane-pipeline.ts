// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LaneView } from '$lib/server/lanes';

export type PipelineStage = 'ingress' | 'outgress' | 'system';
export type PipelineTraffic = 'stream' | 'standard' | 'premium' | 'system';
export type PipelineStageFilter = 'all' | 'twitch' | PipelineStage;
export type PipelineTrafficFilter = 'all' | Exclude<PipelineTraffic, 'system'>;

/** Whether two NATS subject filters share a possible subject. A terminal >
 * matches one or more tokens; * matches exactly one token. */
function overlaps(a: string, b: string): boolean {
  const left = a.split('.');
  const right = b.split('.');
  let i = 0;
  while (i < left.length && i < right.length) {
    if (left[i] === '>' || right[i] === '>') return true;
    if (left[i] !== '*' && right[i] !== '*' && left[i] !== right[i]) return false;
    i++;
  }
  return i === left.length && i === right.length;
}

function ownerStage(lane: LaneView): PipelineStage | null {
  // Sesame deliberately retains the former worker durable group. Subject
  // tokens and optional pod identity follow it (pkg/bus/durableName).
  if ((lane.stream === 'TWITCH_INGRESS' || lane.stream === 'TWITCH_INGRESS_STANDARD') &&
      /^(worker|sesame)(?:_|$)/.test(lane.consumer)) return 'ingress';
  if (lane.stream === 'TWITCH_OUTGRESS' && /^outgress-(premium|standard)(?:_|$)/.test(lane.consumer)) return 'outgress';
  if (lane.stream === 'TWITCH_OUTGRESS_SYSTEM' && /^outgress-system(?:_|$)/.test(lane.consumer)) return 'system';
  return null;
}

export function pipelineTraffic(lane: LaneView): PipelineTraffic[] {
  const stage = ownerStage(lane);
  if (!stage) return [];
  const subjects = lane.subject.split(',').map((subject) => subject.trim()).filter(Boolean);
  // No filter means the consumer sees its entire stream.
  if (subjects.length === 0) subjects.push('>');
  const matches = (...filters: string[]) => subjects.some((subject) => filters.some((filter) => overlaps(subject, filter)));
  if (stage === 'system') return matches('twitch.outgress.system') ? ['system'] : [];
  if (stage === 'outgress') {
    return (['standard', 'premium'] as const).filter((traffic) => matches(`twitch.outgress.${traffic}`));
  }
  const traffic: PipelineTraffic[] = [];
  if (lane.stream === 'TWITCH_INGRESS' && matches('twitch.ingress.event.stream', 'twitch.ingress.status.>')) traffic.push('stream');
  if (matches('twitch.ingress.event.standard', 'twitch.ingress.v2.standard.>')) traffic.push('standard');
  if (lane.stream === 'TWITCH_INGRESS' && matches('twitch.ingress.event.premium', 'twitch.ingress.v2.premium.>')) traffic.push('premium');
  return traffic;
}

export function pipelineStage(lane: LaneView): PipelineStage | null {
  return pipelineTraffic(lane).length > 0 ? ownerStage(lane) : null;
}

export function matchesPipeline(lane: LaneView, stage: PipelineStageFilter, traffic: PipelineTrafficFilter): boolean {
  if (stage === 'all' && traffic === 'all') return true;
  const laneStage = pipelineStage(lane);
  if (!laneStage) return false;
  if (stage !== 'all' && stage !== 'twitch' && stage !== laneStage && !(stage === 'outgress' && laneStage === 'system')) return false;
  return traffic === 'all' || pipelineTraffic(lane).includes(traffic);
}
