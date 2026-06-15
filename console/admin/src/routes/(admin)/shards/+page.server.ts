import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import { shardSnapshot, shardScale, shardAutoscale } from '$lib/server/rpc';
import { requireAdmin, isDemo } from '$lib/server/access';
import { sampleSnapshot } from '$lib/server/sample';

export const load: PageServerLoad = async () => {
  if (isDemo()) return { snapshot: sampleSnapshot, degraded: false };
  try {
    return { snapshot: await shardSnapshot(), degraded: false };
  } catch {
    return { snapshot: sampleSnapshot, degraded: true };
  }
};

export const actions: Actions = {
  scale: async ({ request, locals }) => {
    if (!(await requireAdmin(locals.session))) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const raw = String(f.get('count') ?? '').trim();
    const count = parseInt(raw, 10);
    if (!raw || isNaN(count) || count < 1) return fail(400, { error: 'count must be a positive integer' });
    if (isDemo()) return { action: { ok: true, notice: `scale to ${count} (demo)`, snapshot: sampleSnapshot } };
    try {
      const snapshot = await shardScale(count);
      return { action: { ok: true, notice: `scaled to ${snapshot.desired_count}` }, snapshot };
    } catch (e) {
      return fail(500, { error: (e as Error).message });
    }
  },

  autoscale: async ({ request, locals }) => {
    if (!(await requireAdmin(locals.session))) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const enabled = f.get('enabled') === 'true';
    if (isDemo()) return { action: { ok: true, notice: `autoscale ${enabled ? 'on' : 'off'} (demo)`, snapshot: sampleSnapshot } };
    try {
      const snapshot = await shardAutoscale(enabled);
      return { action: { ok: true, notice: `autoscale ${snapshot.autoscale ? 'enabled' : 'disabled'}` }, snapshot };
    } catch (e) {
      return fail(500, { error: (e as Error).message });
    }
  }
};
