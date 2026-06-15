import type { PageServerLoad } from './$types';
import { shardSnapshot, userStats, tokenStatus } from '$lib/server/rpc';
import { isDemo } from '$lib/server/access';
import { sampleSnapshot, sampleStats } from '$lib/server/sample';
import { env } from '$env/dynamic/private';

export const load: PageServerLoad = async () => {
  if (isDemo()) {
    return { stats: sampleStats, snapshot: sampleSnapshot, botPresent: true, degraded: false };
  }

  let degraded = false;
  let stats = sampleStats;
  let snapshot = sampleSnapshot;
  let botPresent = false;

  try {
    stats = await userStats();
  } catch {
    degraded = true;
  }
  try {
    snapshot = await shardSnapshot();
  } catch {
    degraded = true;
  }

  const botId = env.BOT_USER_ID ?? '';
  if (botId) {
    try {
      botPresent = (await tokenStatus(botId)).present;
    } catch {
      degraded = true;
    }
  }

  return { stats, snapshot, botPresent, degraded };
};
