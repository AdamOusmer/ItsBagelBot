import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';

export const load: PageServerLoad = ({ locals, url }) => {
  if (locals.session && !url.searchParams.has('e')) throw redirect(302, '/');
  return {};
};
