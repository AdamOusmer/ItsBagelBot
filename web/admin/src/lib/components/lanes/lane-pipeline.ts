// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LaneCategory, LaneView } from '#lib/server/lanes';

type PipelineStage = 'ingress' | 'outgress' | 'system';
type PipelineTraffic = 'stream' | 'standard' | 'premium' | 'system';
export type PipelineStageFilter = 'all' | 'twitch' | PipelineStage;
export type PipelineTrafficFilter = 'all' | Exclude<PipelineTraffic, 'system'>;

type TokenPair = readonly [left: string, right: string];
type Subject = readonly string[];
type SubjectPair = readonly [subject: Subject, filter: Subject];

function compatibleTokens(pair: TokenPair): boolean {
  const [left, right] = pair;
  if (left === '*') return true;
  if (right === '*') return true;
  return left === right;
}

function overlaps([left, right]: SubjectPair): boolean {
  const sharedLength = Math.min(left.length, right.length);
  for (let i = 0; i < sharedLength; i++) {
    if (left[i] === '>' || right[i] === '>') return true;
    if (!compatibleTokens([left[i], right[i]])) return false;
  }
  return left.length === right.length;
}

interface TrafficRule {
  traffic: PipelineTraffic;
  subjects: readonly Subject[];
}

interface StreamPipeline {
  stage: PipelineStage;
  owner: RegExp;
  traffic: readonly TrafficRule[];
}

function trafficRule(traffic: PipelineTraffic, ...subjects: string[]): TrafficRule {
  return { traffic, subjects: subjects.map((subject) => subject.split('.')) };
}

const STANDARD = trafficRule('standard', 'twitch.ingress.event.standard', 'twitch.ingress.v2.standard.>');
const STREAM_PIPELINES: Record<string, StreamPipeline> = {
  TWITCH_INGRESS: {
    stage: 'ingress', owner: /^(worker|sesame)(?:_|$)/,
    traffic: [
      trafficRule('stream', 'twitch.ingress.event.stream', 'twitch.ingress.status.>'),
      STANDARD,
      trafficRule('premium', 'twitch.ingress.event.premium', 'twitch.ingress.v2.premium.>')
    ]
  },
  TWITCH_INGRESS_STANDARD: {
    stage: 'ingress', owner: /^(worker|sesame)(?:_|$)/, traffic: [STANDARD]
  },
  TWITCH_OUTGRESS: {
    stage: 'outgress', owner: /^outgress-(premium|standard)(?:_|$)/,
    traffic: [
      trafficRule('standard', 'twitch.outgress.standard'),
      trafficRule('premium', 'twitch.outgress.premium')
    ]
  },
  TWITCH_OUTGRESS_SYSTEM: {
    stage: 'system', owner: /^outgress-system(?:_|$)/,
    traffic: [trafficRule('system', 'twitch.outgress.system')]
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

function consumerSubjects(subject: string): Subject[] {
  const subjects = subject.split(',').map((value) => value.trim()).filter(Boolean);
  return subjects.length ? subjects.map((value) => value.split('.')) : [['>']];
}

type LanePipeline = { stage: PipelineStage; traffic: Set<PipelineTraffic> };

function matchesTraffic(rule: TrafficRule, subjects: Subject[]): boolean {
  return rule.subjects.some((filter) => subjects.some((subject) => overlaps([subject, filter])));
}

function lanePipeline(lane: LaneView): LanePipeline | null {
  const pipeline = ownedPipeline(lane);
  if (!pipeline) return null;
  const subjects = consumerSubjects(lane.subject);
  const traffic = new Set(pipeline.traffic.filter((rule) => matchesTraffic(rule, subjects)).map((rule) => rule.traffic));
  return { stage: pipeline.stage, traffic };
}

function matchesClassification(pipeline: LanePipeline | null, stage: PipelineStageFilter, traffic: PipelineTrafficFilter): boolean {
  if (stage === 'all' && traffic === 'all') return true;
  if (!pipeline || !STAGE_FILTERS[stage].includes(pipeline.stage)) return false;
  return traffic === 'all' ? pipeline.traffic.size > 0 : pipeline.traffic.has(traffic);
}

export function matchesPipeline(lane: LaneView, stage: PipelineStageFilter, traffic: PipelineTrafficFilter): boolean {
  if (stage === 'all' && traffic === 'all') return true;
  return matchesClassification(lanePipeline(lane), stage, traffic);
}

export type LaneFilters = {
  query: string;
  categories: ReadonlySet<string>;
  stage: PipelineStageFilter;
  traffic: PipelineTrafficFilter;
};

function matchesSearch(lane: LaneView, query: string): boolean {
  if (!query) return true;
  return [lane.display, lane.stream, lane.consumer, lane.subject]
    .some((field) => field.toLowerCase().includes(query));
}

function countMatches<Value extends string>(counts: Record<Value, number>, matches: (choice: Value) => boolean) {
  for (const choice in counts) {
    counts[choice] += Number(matches(choice));
  }
}

export function filterLanes(lanes: readonly LaneView[], filters: LaneFilters) {
  const { categories, stage, traffic } = filters;
  const query = filters.query.trim().toLowerCase();
  const rows: LaneView[] = [];
  const categoryCounts: Record<LaneCategory | 'all', number> = { all: 0, system: 0, projection: 0, ephemeral: 0 };
  const stageCounts: Record<PipelineStageFilter, number> = { all: 0, twitch: 0, ingress: 0, outgress: 0, system: 0 };
  const trafficCounts: Record<PipelineTrafficFilter, number> = { all: 0, stream: 0, standard: 0, premium: 0 };

  lanes.forEach((lane) => {
    if (!matchesSearch(lane, query)) return;
    const pipeline = lanePipeline(lane);
    const matchesPath = matchesClassification(pipeline, stage, traffic);
    categoryCounts.all += Number(matchesPath);
    categoryCounts[lane.category] += Number(matchesPath);
    if (categories.size > 0 && !categories.has(lane.category)) return;
    if (matchesPath) rows.push(lane);
    countMatches(stageCounts, (choice) => matchesClassification(pipeline, choice, traffic));
    countMatches(trafficCounts, (choice) => matchesClassification(pipeline, stage, choice));
  });

  return { rows, categoryCounts, stageCounts, trafficCounts };
}
