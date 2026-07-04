import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';

// The per-module detail page was folded into the modules deck's docked inspector
// (routes/(app)/modules/+page.svelte), so this route no longer renders its own
// page. Keep old deep links working by bouncing them to the list; the config
// now opens in the inspector there. 308 preserves the method and is permanent.
export const load: PageServerLoad = () => {
  throw redirect(308, '/modules');
};
