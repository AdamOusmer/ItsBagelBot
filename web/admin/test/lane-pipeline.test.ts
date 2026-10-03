// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import type { LaneView } from '../src/lib/server/lanes';
import { matchesPipeline, type PipelineTrafficFilter } from '../src/lib/components/lanes/lane-pipeline';

function lane(stream: string, consumer: string, subject: string): LaneView {
  return { stream, consumer, subject, display: 'operator alias', category: 'projection', ephemeral: false, orphan: false, pending: 0, inFlight: '0', rate: '-', redelivered: 0 };
}

const TRAFFIC: Exclude<PipelineTrafficFilter, 'all'>[] = ['stream', 'standard', 'premium'];
const trafficOf = (row: LaneView) => TRAFFIC.filter((traffic) => matchesPipeline(row, 'twitch', traffic));
const inTwitchPipeline = (row: LaneView) => matchesPipeline(row, 'twitch', 'all');

describe('Twitch processing pipeline filters', () => {
  test('Sesame retains worker-owned legacy and v2 premium/standard lanes', () => {
    for (const traffic of ['premium', 'standard'] as const) {
      for (const subject of [`twitch.ingress.event.${traffic}`, `twitch.ingress.v2.${traffic}.>`, `twitch.ingress.v2.${traffic}.42`]) {
        const row = lane(traffic === 'standard' ? 'TWITCH_INGRESS_STANDARD' : 'TWITCH_INGRESS', `worker_${subject.replace(/[.*>]/g, '_')}_sesame-pod`, subject);
        expect(matchesPipeline(row, 'ingress', 'all')).toBe(true);
        expect(trafficOf(row)).toEqual([traffic]);
        expect(matchesPipeline(row, 'ingress', traffic)).toBe(true);
      }
    }
    expect(matchesPipeline(lane('TWITCH_INGRESS', 'sesame_twitch_ingress_event_standard', 'twitch.ingress.event.standard'), 'ingress', 'standard')).toBe(true);
  });

  test('stream and status lanes are stream traffic when owned by Sesame', () => {
    for (const subject of ['twitch.ingress.event.stream', 'twitch.ingress.status.>', 'twitch.ingress.status.authz.granted']) {
      expect(trafficOf(lane('TWITCH_INGRESS', 'worker_stream', subject))).toEqual(['stream']);
    }
  });

  test('excludes outgress observers, automod, projection and lookalike groups', () => {
    for (const consumer of ['outgress_twitch_ingress_event_stream', 'projector_twitch_ingress_event_standard', 'automod_twitch_ingress_v2_premium__', 'worker-metrics_twitch_ingress_event_premium', 'sesame-backup_twitch_ingress_event_standard']) {
      expect(inTwitchPipeline(lane('TWITCH_INGRESS', consumer, 'twitch.ingress.>'))).toBe(false);
    }
    expect(inTwitchPipeline(lane('TWITCH_INGRESS_RETRY', 'worker_retry', 'twitch.ingress.retry.premium'))).toBe(false);
  });

  test('scopes the outgoing lanes to actual outgress owners and exact streams', () => {
    for (const traffic of ['premium', 'standard'] as const) {
      const row = lane('TWITCH_OUTGRESS', `outgress-${traffic}_twitch_outgress_${traffic}`, `twitch.outgress.${traffic}`);
      expect(matchesPipeline(row, 'outgress', traffic)).toBe(true);
    }
    const system = lane('TWITCH_OUTGRESS_SYSTEM', 'outgress-system_twitch_outgress_system', 'twitch.outgress.system');
    expect(matchesPipeline(system, 'system', 'all')).toBe(true);
    expect(matchesPipeline(system, 'outgress', 'all')).toBe(true);
    expect(matchesPipeline(system, 'outgress', 'standard')).toBe(false);
    expect(matchesPipeline(system, 'twitch', 'premium')).toBe(false);
    for (const row of [lane('TWITCH_OUTGRESS_SYSTEM_EXTRA', 'outgress-system_job', 'twitch.outgress.system'), lane('TWITCH_OUTGRESS', 'projector_job', 'twitch.outgress.premium'), lane('TWITCH_AUTOMOD_ACTION', 'automod-action_job', 'twitch.automod.action.>')]) {
      expect(inTwitchPipeline(row)).toBe(false);
    }
  });

  test('supports comma-separated filters and wildcards without substring matches', () => {
    expect(trafficOf(lane('TWITCH_INGRESS', 'worker_multi', ' twitch.ingress.v2.premium.*, twitch.ingress.event.standard, twitch.ingress.status.> '))).toEqual(['stream', 'standard', 'premium']);
    expect(trafficOf(lane('TWITCH_INGRESS', 'worker_all', 'twitch.ingress.>'))).toEqual(['stream', 'standard', 'premium']);
    expect(trafficOf(lane('TWITCH_INGRESS_STANDARD', 'worker_all', ''))).toEqual(['standard']);
    expect(trafficOf(lane('TWITCH_OUTGRESS', 'outgress-premium_all', 'twitch.outgress.*'))).toEqual(['standard', 'premium']);
    for (const subject of ['twitch.ingress.v2.premium', 'twitch.ingress.status', 'twitch.ingress.event.premium-extra', 'other.twitch.ingress.event.premium']) {
      expect(inTwitchPipeline(lane('TWITCH_INGRESS', 'worker_invalid', subject))).toBe(false);
    }
  });

  test('all preserves unrelated lanes while traffic selections target the pipeline', () => {
    const unrelated = lane('DATA', 'projector_changes', 'bagel.data.>');
    expect(matchesPipeline(unrelated, 'all', 'all')).toBe(true);
    expect(matchesPipeline(unrelated, 'all', 'standard')).toBe(false);
    const premium = lane('TWITCH_INGRESS', 'worker_premium', 'twitch.ingress.event.premium');
    expect(matchesPipeline(premium, 'twitch', 'all')).toBe(true);
    expect(matchesPipeline(premium, 'all', 'premium')).toBe(true);
    expect(matchesPipeline(premium, 'system', 'all')).toBe(false);
  });
});
