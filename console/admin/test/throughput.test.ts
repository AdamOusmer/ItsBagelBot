// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
import type { ShardSnapshot } from '@bagel/shared';
import {
  barWidth,
  eventsPerSecond,
  resolveCapacity,
  utilizationPct,
  utilizationTone
} from '../src/lib/throughput';

function snapshot(overrides: Partial<ShardSnapshot> = {}): ShardSnapshot {
  return {
    generated_at: new Date(0).toISOString(),
    reporter: 'ingress@test',
    nodes: ['one', 'two'],
    shard_count: 2,
    shards: [],
    desired_count: 2,
    target: 2,
    min_shards: 2,
    autoscale: true,
    ...overrides
  };
}

describe('throughput capacity', () => {
  test('rolling-deploy fallback scales the measured pod rating by live nodes', () => {
    const capacity = resolveCapacity(snapshot());

    expect(capacity.fleet_rated_eps).toBe(280_000);
    expect(capacity.fleet_target_eps).toBe(210_000);
    expect(capacity.effective_rated_eps).toBe(123_000);
    expect(capacity.effective_target_eps).toBe(92_250);
    expect(capacity.bottleneck).toBe('nats');
    expect(capacity.websocket_target_eps).toBe(9_375);
    expect(capacity.target_utilization_pct).toBe(75);
  });

  test('uses capacity supplied by ingress as the source of truth', () => {
    const supplied = {
      ...resolveCapacity(snapshot()),
      pod_rated_eps: 200_000,
      fleet_rated_eps: 400_000
    };

    expect(resolveCapacity(snapshot({ capacity: supplied }))).toBe(supplied);
  });

  test('percentage and bar share the same rated denominator', () => {
    const rate = eventsPerSecond(420_000, 60);
    const utilization = utilizationPct(rate, 12_500);

    expect(rate).toBe(7_000);
    expect(utilization).toBeCloseTo(56);
    expect(barWidth(utilization)).toBe(56);
  });

  test('75% is the scale target, with warning beginning at 60%', () => {
    expect(utilizationTone(59.9, 75)).toBe('green');
    expect(utilizationTone(60, 75)).toBe('warn');
    expect(utilizationTone(75, 75)).toBe('err');
  });
});
