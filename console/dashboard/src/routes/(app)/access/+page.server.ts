import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';

// Share management moved to /settings. Keep the old URL working with a redirect.
export const load: PageServerLoad = async () => {
  throw redirect(302, '/settings');
};
