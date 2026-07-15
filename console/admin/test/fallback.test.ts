// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
import { EMPTY_USER_STATS, emptyEnrollment, emptyShardSnapshot } from '../src/lib/server/fallback';

describe('admin production outage fallbacks', () => {
  test('uses zero-valued enrollment buckets and stats', () => {
    const enrollment = emptyEnrollment(7);
    expect(enrollment.days).toHaveLength(7);
    expect(enrollment.days.every((day) => day.count === 0)).toBe(true);
    expect(enrollment.stats).toEqual(EMPTY_USER_STATS);
  });

  test('uses an empty fleet snapshot', () => {
    expect(emptyShardSnapshot()).toEqual({
      generated_at: '',
      reporter: '',
      nodes: [],
      shard_count: 0,
      shards: [],
      desired_count: 0,
      target: 0,
      min_shards: 0,
      max_shards: 0,
      autoscale: false
    });
  });
});
