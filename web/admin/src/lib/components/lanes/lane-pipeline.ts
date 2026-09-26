// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LaneView } from '$lib/server/lanes';

export type PipelineStage = 'ingress' | 'outgress' | 'system';
export type PipelineTraffic = 'stream' | 'standard' | 'premium' | 'system';
export type PipelineStageFilter = 'all' | 'twitch' | PipelineStage;
export type PipelineTrafficFilter = 'all' | Exclude<PipelineTraffic, 'system'>;

function compatibleTokens(left: string, right: string): boolean {
  if (left === '*') return true;
  if (right === '*') return true;
  return left === right;
}

/** Whether two NATS subject filters share a possible subject. A terminal >
 * matches one or more tokens; * matches exactly one token. */
function overlaps(a: string, b: string): boolean {
  const left = a.split('.');
  const right = b.split('.');
  const sharedLength = Math.min(left.length, right.length);
  for (let i = 0; i < sharedLength; i++) {
    if ([left[i], right[i]].includes('>')) return true;
    if (!compatibleTokens(left[i], right[i])) return false;
  }
  return left.length === right.length;
}

interface TrafficRule {
  traffic: PipelineTraffic;
  subjects: readonly string[];
}

interface StreamPipeline {
  stage: PipelineStage;
  owner: RegExp;
  traffic: readonly TrafficRule[];
}

const STANDARD: TrafficRule = {
  traffic: 'standard', subjects: ['twitch.ingress.event.standard', 'twitch.ingress.v2.standard.>']
};

// Sesame retains the worker durable group. Subject tokens and optional pod
// identity follow it (pkg/bus/durableName).
const STREAM_PIPELINES: Record<string, StreamPipeline> = {
  TWITCH_INGRESS: {
    stage: 'ingress', owner: /^(worker|sesame)(?:_|$)/,
    traffic: [
      { traffic: 'stream', subjects: ['twitch.ingress.event.stream', 'twitch.ingress.status.>'] },
      STANDARD,
      { traffic: 'premium', subjects: ['twitch.ingress.event.premium', 'twitch.ingress.v2.premium.>'] }
    ]
  },
  TWITCH_INGRESS_STANDARD: {
    stage: 'ingress', owner: /^(worker|sesame)(?:_|$)/, traffic: [STANDARD]
  },
  TWITCH_OUTGRESS: {
    stage: 'outgress', owner: /^outgress-(premium|standard)(?:_|$)/,
    traffic: [
      { traffic: 'standard', subjects: ['twitch.outgress.standard'] },
      { traffic: 'premium', subjects: ['twitch.outgress.premium'] }
    ]
  },
  TWITCH_OUTGRESS_SYSTEM: {
    stage: 'system', owner: /^outgress-system(?:_|$)/,
    traffic: [{ traffic: 'system', subjects: ['twitch.outgress.system'] }]
  }
};

const STAGE_FILTERS: Record<PipelineStageFilter, readonly PipelineStage[]> = {
  all: ['ingress', 'outgress', 'system'],
  twitch: ['ingress', 'outgress', 'system'],
  ingress: ['ingress'],
  outgress: ['outgress', 'system'],
  system: ['system']
};

function ownedPipeline(lane: LaneView): StreamPipeline | null {
  if (!Object.hasOwn(STREAM_PIPELINES, lane.stream)) return null;
  const pipeline = STREAM_PIPELINES[lane.stream];
  return pipeline.owner.test(lane.consumer) ? pipeline : null;
}

function consumerSubjects(subject: string): string[] {
  const subjects = subject.split(',').map((value) => value.trim()).filter(Boolean);
  // No filter means the consumer sees its entire stream.
  return subjects.length > 0 ? subjects : ['>'];
}

function subjectsOverlap(subjects: string[], filters: readonly string[]): boolean {
  return subjects.some((subject) => filters.some((filter) => overlaps(subject, filter)));
}

export function pipelineTraffic(lane: LaneView): PipelineTraffic[] {
  const pipeline = ownedPipeline(lane);
  if (!pipeline) return [];
  const subjects = consumerSubjects(lane.subject);
  return pipeline.traffic
    .filter((rule) => subjectsOverlap(subjects, rule.subjects))
    .map((rule) => rule.traffic);
}

export function pipelineStage(lane: LaneView): PipelineStage | null {
  if (pipelineTraffic(lane).length === 0) return null;
  return ownedPipeline(lane)?.stage ?? null;
}

export function matchesPipeline(lane: LaneView, stage: PipelineStageFilter, traffic: PipelineTrafficFilter): boolean {
  if (stage === 'all' && traffic === 'all') return true;
  const laneStage = pipelineStage(lane);
  if (!laneStage) return false;
  if (!STAGE_FILTERS[stage].includes(laneStage)) return false;
  return traffic === 'all' || pipelineTraffic(lane).includes(traffic);
}
