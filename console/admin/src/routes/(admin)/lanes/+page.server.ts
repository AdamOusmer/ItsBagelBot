import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import { loadLanes, laneAlias, laneDurable, laneDelete } from '$lib/server/lanes';
import { requireAdmin, isDemo } from '$lib/server/access';

export const load: PageServerLoad = async () => {
  return loadLanes();
};

// The three lane mutations are wired as form actions. Each is gated on an admin
// session, is a no-op under DEMO (no live broker), and surfaces the RPC reply's
// notice/error back to the page.
export const actions: Actions = {
  alias: async ({ request, locals }) => {
    if (!(await requireAdmin(locals.session))) return fail(403, { notice: 'forbidden' });
    if (isDemo()) return { ok: false, notice: 'demo mode: lane mutations are disabled' };
    const form = await request.formData();
    const stream = String(form.get('stream') ?? '');
    const consumer = String(form.get('consumer') ?? '');
    const alias = String(form.get('alias') ?? '');
    try {
      return await laneAlias(stream, consumer, alias);
    } catch (e) {
      return fail(502, { notice: `rename failed: ${(e as Error).message}` });
    }
  },
  durable: async ({ request, locals }) => {
    if (!(await requireAdmin(locals.session))) return fail(403, { notice: 'forbidden' });
    if (isDemo()) return { ok: false, notice: 'demo mode: lane mutations are disabled' };
    const form = await request.formData();
    const stream = String(form.get('stream') ?? '');
    const consumer = String(form.get('consumer') ?? '');
    try {
      return await laneDurable(stream, consumer);
    } catch (e) {
      return fail(502, { notice: `make-permanent failed: ${(e as Error).message}` });
    }
  },
  delete: async ({ request, locals }) => {
    if (!(await requireAdmin(locals.session))) return fail(403, { notice: 'forbidden' });
    if (isDemo()) return { ok: false, notice: 'demo mode: lane mutations are disabled' };
    const form = await request.formData();
    const stream = String(form.get('stream') ?? '');
    const consumer = String(form.get('consumer') ?? '');
    try {
      return await laneDelete(stream, consumer);
    } catch (e) {
      return fail(502, { notice: `delete failed: ${(e as Error).message}` });
    }
  }
};
