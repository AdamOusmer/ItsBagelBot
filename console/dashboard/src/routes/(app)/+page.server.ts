import type { PageServerLoad } from './$types';
import { hasGrant, isActive } from '$lib/server/rpc';
import { env } from '$env/dynamic/private';

export const load: PageServerLoad = async ({ locals }) => {
  const uid = locals.session?.user_id ?? 'demo';

  // Live bot state from RPC; degrade gracefully so the page still renders if
  // the provider is briefly unreachable.
  let enabled = false;
  let receiving = false;
  if (env.DEMO !== '1') {
    try {
      enabled = await hasGrant(uid);
      receiving = enabled && (await isActive(uid));
    } catch {
      /* keep defaults */
    }
  } else {
    enabled = true;
    receiving = true;
  }

  return { enabled, receiving };
};
