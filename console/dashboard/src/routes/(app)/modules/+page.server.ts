import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';

// Modules has no data RPC yet; this load exists only to gate a delegate who was
// not granted the 'modules' section. Owners and full sessions pass through.
export const load: PageServerLoad = async ({ locals }) => {
  const s = locals.session;
  if (s?.delegate_of && !(s.sections ?? []).includes('modules')) {
    throw redirect(302, '/');
  }
  return {};
};
