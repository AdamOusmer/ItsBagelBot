import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import { loadLanes, laneMutationUnavailable } from '$lib/server/lanes';
import { allowed } from '$lib/server/access';

export const load: PageServerLoad = async () => {
  return loadLanes();
};

// The three lane mutations are wired as form actions to mirror the old admin,
// but there is no JetStream-management RPC subject to call from the console, so
// each returns an honest "unavailable" notice instead of guessing wire formats.
export const actions: Actions = {
  alias: async ({ locals }) => {
    if (!allowed(locals.session)) return fail(403, { notice: 'forbidden' });
    return { ok: false, notice: laneMutationUnavailable('Rename') };
  },
  durable: async ({ locals }) => {
    if (!allowed(locals.session)) return fail(403, { notice: 'forbidden' });
    return { ok: false, notice: laneMutationUnavailable('Make-permanent') };
  },
  delete: async ({ locals }) => {
    if (!allowed(locals.session)) return fail(403, { notice: 'forbidden' });
    return { ok: false, notice: laneMutationUnavailable('Delete') };
  }
};
