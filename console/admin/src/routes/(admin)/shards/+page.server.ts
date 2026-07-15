import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { shardSnapshot, shardScale, shardAutoscale } from '$lib/server/services';
import { requireAdmin } from '$lib/server/access';
import { emptyShardSnapshot } from '$lib/server/fallback';

import type { ShardSnapshot } from '@bagel/shared';

export type ShardsBundle = { snapshot: ShardSnapshot; degraded: boolean };
const DEMO = dev && process.env.DEMO === '1';

// Streamed: the shell renders immediately; the snapshot hydrates when the
// ingress RPC lands. A failure returns a neutral empty snapshot and says so.
export const load: PageServerLoad = () => {
  const bundle: Promise<ShardsBundle> = DEMO
    ? import('$lib/server/demo-data').then(({ sampleSnapshot }) => ({
        snapshot: sampleSnapshot,
        degraded: false
      }))
    : shardSnapshot()
        .then((snapshot) => ({ snapshot, degraded: false }))
        .catch(() => ({ snapshot: emptyShardSnapshot(), degraded: true }));
  return { bundle };
};

export const actions: Actions = {
  scale: async ({ request, locals }) => {
    if (!(await requireAdmin(locals.session))) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const raw = String(f.get('count') ?? '').trim();
    const count = parseInt(raw, 10);
    if (!raw || isNaN(count) || count < 1) return fail(400, { error: 'count must be a positive integer' });
    if (DEMO) {
      const { sampleSnapshot } = await import('$lib/server/demo-data');
      return { action: { ok: true, notice: `scale to ${count} (demo)`, snapshot: sampleSnapshot } };
    }
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
    if (DEMO) {
      const { sampleSnapshot } = await import('$lib/server/demo-data');
      return { action: { ok: true, notice: `autoscale ${enabled ? 'on' : 'off'} (demo)`, snapshot: sampleSnapshot } };
    }
    try {
      const snapshot = await shardAutoscale(enabled);
      return { action: { ok: true, notice: `autoscale ${snapshot.autoscale ? 'enabled' : 'disabled'}` }, snapshot };
    } catch (e) {
      return fail(500, { error: (e as Error).message });
    }
  }
};
