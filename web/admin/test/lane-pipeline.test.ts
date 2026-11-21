// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import type { LaneView } from '../src/lib/server/lanes';
import {
  filterLanes,
  matchesPipeline,
  type LaneFilters,
  type PipelineStageFilter,
  type PipelineTrafficFilter
} from '../src/lib/components/lanes/lane-pipeline';

function lane(stream: string, consumer: string, subject: string): LaneView {
  return { stream, consumer, subject, display: 'operator alias', category: 'projection', ephemeral: false, orphan: false, pending: 0, inFlight: '0', rate: '-', redelivered: 0 };
}

type Traffic = Exclude<PipelineTrafficFilter, 'all'>;
const TRAFFIC: Traffic[] = ['stream', 'standard', 'premium'];
const INGRESS = 'TWITCH_INGRESS';
const STANDARD_INGRESS = 'TWITCH_INGRESS_STANDARD';
const OUTGRESS = 'TWITCH_OUTGRESS';
const SYSTEM = 'TWITCH_OUTGRESS_SYSTEM';

describe('Twitch processing pipeline filters', () => {
  test.each([
    [INGRESS, 'worker_premium', 'twitch.ingress.event.premium', ['premium']],
    [INGRESS, 'worker_premium', 'twitch.ingress.v2.premium.>', ['premium']],
    [INGRESS, 'worker_premium', 'twitch.ingress.v2.premium.42', ['premium']],
    [STANDARD_INGRESS, 'worker_standard', 'twitch.ingress.event.standard', ['standard']],
    [STANDARD_INGRESS, 'worker_standard', 'twitch.ingress.v2.standard.>', ['standard']],
    [STANDARD_INGRESS, 'worker_standard', 'twitch.ingress.v2.standard.42', ['standard']],
    [INGRESS, 'sesame_standard', 'twitch.ingress.event.standard', ['standard']],
    [INGRESS, 'worker_stream', 'twitch.ingress.event.stream', ['stream']],
    [INGRESS, 'worker_stream', 'twitch.ingress.status.>', ['stream']],
    [INGRESS, 'worker_stream', 'twitch.ingress.status.authz.granted', ['stream']],
    [INGRESS, 'worker_multi', ' twitch.ingress.v2.premium.*, twitch.ingress.event.standard, twitch.ingress.status.> ', TRAFFIC],
    [INGRESS, 'worker_all', 'twitch.ingress.>', TRAFFIC],
    [STANDARD_INGRESS, 'worker_all', '', ['standard']],
    [OUTGRESS, 'outgress-standard_main', 'twitch.outgress.standard', ['standard']],
    [OUTGRESS, 'outgress-premium_main', 'twitch.outgress.premium', ['premium']],
    [OUTGRESS, 'outgress-premium_all', 'twitch.outgress.*', ['standard', 'premium']]
  ] satisfies [string, string, string, Traffic[]][])('classifies traffic for %s / %s / %s', (stream, consumer, subject, expected) => {
    const row = lane(stream, consumer, subject);
    const stage = stream === OUTGRESS ? 'outgress' : 'ingress';
    expect(TRAFFIC.filter((traffic) => matchesPipeline(row, stage, traffic))).toEqual(expected);
    expect(matchesPipeline(row, 'twitch', 'all')).toBe(true);
  });

  test.each([
    [INGRESS, 'outgress_twitch_ingress_event_stream', 'twitch.ingress.>'],
    [INGRESS, 'projector_twitch_ingress_event_standard', 'twitch.ingress.>'],
    [INGRESS, 'automod_twitch_ingress_v2_premium__', 'twitch.ingress.>'],
    [INGRESS, 'worker-metrics_twitch_ingress_event_premium', 'twitch.ingress.>'],
    [INGRESS, 'sesame-backup_twitch_ingress_event_standard', 'twitch.ingress.>'],
    ['TWITCH_INGRESS_RETRY', 'worker_retry', 'twitch.ingress.retry.premium'],
    ['TWITCH_OUTGRESS_SYSTEM_EXTRA', 'outgress-system_job', 'twitch.outgress.system'],
    ['TWITCH_AUTOMOD_ACTION', 'automod-action_job', 'twitch.automod.action.>'],
    [OUTGRESS, 'projector_job', 'twitch.outgress.premium'],
    [INGRESS, 'worker_invalid', 'twitch.ingress.v2.premium'],
    [INGRESS, 'worker_invalid', 'twitch.ingress.status'],
    [INGRESS, 'worker_invalid', 'twitch.ingress.event.premium-extra'],
    [INGRESS, 'worker_invalid', 'other.twitch.ingress.event.premium']
  ])('excludes nonpipeline consumer %s / %s / %s', (stream, consumer, subject) => {
    expect(matchesPipeline(lane(stream, consumer, subject), 'twitch', 'all')).toBe(false);
  });

  const system = lane(SYSTEM, 'outgress-system_main', 'twitch.outgress.system');
  const premium = lane(INGRESS, 'worker_premium', 'twitch.ingress.event.premium');
  const unrelated = lane('DATA', 'projector_changes', 'data.>');

  test.each([
    [system, 'system', 'all', true],
    [system, 'outgress', 'all', true],
    [system, 'outgress', 'standard', false],
    [system, 'twitch', 'premium', false],
    [premium, 'all', 'premium', true],
    [premium, 'system', 'all', false],
    [unrelated, 'all', 'all', true],
    [unrelated, 'all', 'standard', false]
  ] satisfies [LaneView, PipelineStageFilter, PipelineTrafficFilter, boolean][])(
    'combines stage and traffic for %j / %s / %s',
    (row, stage, traffic, expected) => {
      expect(matchesPipeline(row, stage, traffic)).toBe(expected);
    }
  );
});

describe('lane filter counts', () => {
  const stream = lane('TWITCH_INGRESS', 'worker_stream', 'twitch.ingress.event.stream');
  const premium = lane('TWITCH_INGRESS', 'worker_premium', 'twitch.ingress.event.premium');
  const outgress: LaneView = { ...lane('TWITCH_OUTGRESS', 'outgress-premium_main', 'twitch.outgress.premium'), category: 'system' };
  const unrelated: LaneView = { ...lane('DATA', 'projector_changes', 'data.>'), category: 'ephemeral' };
  const lanes = [stream, premium, outgress, unrelated];
  const defaults: LaneFilters = { query: '', categories: new Set(), stage: 'all', traffic: 'all' };

  test('counts each choice against only the other active filters', () => {
    expect(filterLanes(lanes, { ...defaults, stage: 'ingress', traffic: 'premium', categories: new Set(['system', 'projection']) })).toEqual({
      rows: [premium],
      categoryCounts: { all: 1, projection: 1, system: 0, ephemeral: 0 },
      stageCounts: { all: 2, twitch: 2, ingress: 1, outgress: 1, system: 0 },
      trafficCounts: { all: 2, stream: 1, standard: 0, premium: 1 }
    });
  });

  test.each([
    [defaults, lanes],
    [{ ...defaults, query: ' OUTGRESS ' }, [outgress]],
    [{ ...defaults, categories: new Set(['system', 'ephemeral']) }, [outgress, unrelated]],
    [{ ...defaults, stage: 'twitch' }, [stream, premium, outgress]]
  ] satisfies [LaneFilters, LaneView[]][])('applies search and category selection to rows (%j)', (filters, rows) => {
    expect(filterLanes(lanes, filters).rows).toEqual(rows);
  });
});
