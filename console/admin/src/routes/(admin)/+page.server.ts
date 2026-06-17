import type { PageServerLoad } from './$types';
import { shardSnapshot, userStats, tokenStatus } from '$lib/server/rpc';
import { isDemo } from '$lib/server/access';
import { sampleSnapshot, sampleStats } from '$lib/server/sample';
import { env } from '$env/dynamic/private';
import type { ShardSnapshot, UserStats } from '@bagel/shared';

export type Overview = {
  stats: UserStats;
  snapshot: ShardSnapshot;
  botPresent: boolean;
  degraded: boolean;
};

// Resolve the three independent reads (stats, shard snapshot, bot token) in
// parallel rather than three serial awaits, so the page waits one round trip
// instead of three. allSettled keeps the page rendering even if one responder
// is slow or down; each failure flips the degraded flag and falls back to the
// last-known/sample value.
async function loadOverview(): Promise<Overview> {
  const botId = env.ADMIN_BOT_USER_ID ?? '';
  const [stats, snapshot, token] = await Promise.allSettled([
    userStats(),
    shardSnapshot(),
    botId ? tokenStatus(botId) : Promise.resolve({ present: false })
  ]);

  const degraded =
    stats.status === 'rejected' ||
    snapshot.status === 'rejected' ||
    (botId !== '' && token.status === 'rejected');

  return {
    stats: stats.status === 'fulfilled' ? stats.value : sampleStats,
    snapshot: snapshot.status === 'fulfilled' ? snapshot.value : sampleSnapshot,
    botPresent: token.status === 'fulfilled' && token.value.present,
    degraded
  };
}

export const load: PageServerLoad = () => {
  // Return the bundle as an unawaited promise so SvelteKit streams it: the page
  // shell (and the post-login redirect) renders immediately and the live data
  // hydrates when the round trip lands, instead of blocking SSR on NATS.
  const overview: Promise<Overview> = isDemo()
    ? Promise.resolve({
        stats: sampleStats,
        snapshot: sampleSnapshot,
        botPresent: true,
        degraded: false
      })
    : loadOverview();

  return { overview };
};
