import type { LayoutServerLoad } from './$types';
import { demoSession } from '$lib/server/access';

// Admin gate removed: the admin panel is only accessible on the authorized tailnet.
export const load: LayoutServerLoad = () => {
  const s = demoSession;
  return { displayName: s.display_name, login: s.login };
};
