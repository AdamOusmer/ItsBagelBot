// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import { fail } from '@sveltejs/kit';
import { dev } from '$app/environment';
import {
  loadLanes,
  laneAlias,
  laneDurable,
  laneDelete,
  type LaneMutationResult
} from '$lib/server/lanes';
import { requireRole } from '$lib/server/access';
import { audit } from '$lib/server/audit';
import { adminText } from '$lib/server/admin-action';

const DEMO = dev && process.env.DEMO === '1';

export const load: PageServerLoad = () => {
  return { lanes: loadLanes() };
};

type LaneOp = {
  action: string;
  verb: string;
  run: (stream: string, consumer: string, f: FormData) => Promise<LaneMutationResult>;
};

// The only gate: JetStream sees one shared operator credential, so it cannot stop a moderator.
function laneAction(op: LaneOp) {
  return async ({ request, locals }: RequestEvent) => {
    const admin = await requireRole({ locals }, 'lanes.mutate');
    if (!admin) return fail(403, { notice: adminText(locals.locale, 'admin.action.forbidden') });
    if (DEMO) return { ok: false, notice: adminText(locals.locale, 'admin.action.laneDemoDisabled') };

    const form = await request.formData();
    const stream = String(form.get('stream') ?? '');
    const consumer = String(form.get('consumer') ?? '');
    const target = `${stream}/${consumer}`;
    try {
      const result = await op.run(stream, consumer, form);
      audit(admin, { action: op.action, target, ok: true });
      return result;
    } catch (e) {
      const error = (e as Error).message;
      audit(admin, { action: op.action, target, ok: false, error });
      return fail(502, { notice: adminText(locals.locale, 'admin.action.laneFailed', { verb: op.verb, error }) });
    }
  };
}

export const actions: Actions = {
  alias: laneAction({
    action: 'lane_alias',
    verb: 'rename',
    run: (stream, consumer, f) => laneAlias(stream, consumer, String(f.get('alias') ?? ''))
  }),
  durable: laneAction({
    action: 'lane_durable',
    verb: 'make-permanent',
    run: (stream, consumer) => laneDurable(stream, consumer)
  }),
  delete: laneAction({
    action: 'lane_delete',
    verb: 'delete',
    run: (stream, consumer) => laneDelete(stream, consumer)
  })
};
