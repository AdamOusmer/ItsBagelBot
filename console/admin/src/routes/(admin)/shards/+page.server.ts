import type { PageServerLoad } from './$types';
import { shardSnapshot } from '$lib/server/rpc';
import { isDemo } from '$lib/server/access';
import { sampleSnapshot } from '$lib/server/sample';

export const load: PageServerLoad = async () => {
  if (isDemo()) return { snapshot: sampleSnapshot, degraded: false };
  try {
    return { snapshot: await shardSnapshot(), degraded: false };
  } catch {
    return { snapshot: sampleSnapshot, degraded: true };
  }
};
