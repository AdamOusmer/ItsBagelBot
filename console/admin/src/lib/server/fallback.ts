import type { ShardSnapshot, UserStats } from '@bagel/shared';
import type { EnrollmentWire } from './services';

// Production outage fallbacks are deliberately neutral. They preserve the
// response shapes used by the pages without presenting seeded fixture values
// as if they were live control-plane state.
export const EMPTY_USER_STATS: UserStats = {
  total_users: 0,
  active_users: 0,
  premium_users: 0,
  vip_users: 0,
  paid_users: 0
};

export function emptyEnrollment(days = 30): EnrollmentWire {
  const count = Math.max(1, Math.trunc(days));
  return {
    days: Array.from({ length: count }, (_, i) => {
      const date = new Date(Date.now() - (count - 1 - i) * 864e5);
      return { date: date.toISOString().slice(0, 10), count: 0 };
    }),
    stats: { ...EMPTY_USER_STATS }
  };
}

export function emptyShardSnapshot(): ShardSnapshot {
  return {
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
  };
}
