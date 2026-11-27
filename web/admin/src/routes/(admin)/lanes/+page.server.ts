// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import { fail } from '@sveltejs/kit';
import {
  loadLanes,
  laneAlias,
  laneDurable,
  laneDelete,
  type LaneMutationResult
} from '#lib/server/lanes';
import { requireRole } from '#lib/server/access';
import { audit } from '#lib/server/audit';
import { adminText } from '#lib/server/admin-action';

export const load = (({ depends }) => {
  depends('admin:lanes');
  return { lanes: loadLanes() };
}) satisfies PageServerLoad;

type LaneOp = {
  action: string;
  failureKey: string;
  run: (stream: string, consumer: string, f: FormData) => Promise<LaneMutationResult>;
};

// The only gate: JetStream sees one shared operator credential, so it cannot stop a moderator.
function laneAction(op: LaneOp) {
  return async ({ request, locals }: RequestEvent) => {
    const admin = await requireRole({ locals }, 'lanes.mutate');
    if (!admin) return fail(403, { notice: adminText(locals.locale, 'admin.action.forbidden') });

    const form = await request.formData();
    const stream = String(form.get('stream') ?? '');
    const consumer = String(form.get('consumer') ?? '');
    const target = `${stream}/${consumer}`;
    try {
      const result = await op.run(stream, consumer, form);
      audit(admin, { action: op.action, target, ok: result.ok, error: result.error });
      return result.ok ? result : fail(409, { notice: adminText(locals.locale, op.failureKey) });
    } catch (cause) {
      const error = cause instanceof Error ? cause.message : String(cause);
      audit(admin, { action: op.action, target, ok: false, error });
      return fail(502, { notice: adminText(locals.locale, op.failureKey) });
    }
  };
}

export const actions: Actions = {
  alias: laneAction({
    action: 'lane_alias',
    failureKey: 'admin.lanes.renameFailed',
    run: (stream, consumer, f) => laneAlias(stream, consumer, String(f.get('alias') ?? ''))
  }),
  durable: laneAction({
    action: 'lane_durable',
    failureKey: 'admin.lanes.durableFailed',
    run: laneDurable
  }),
  delete: laneAction({
    action: 'lane_delete',
    failureKey: 'admin.lanes.deleteFailed',
    run: laneDelete
  })
};
