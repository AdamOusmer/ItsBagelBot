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

const DEMO = dev && process.env.DEMO === '1';

// Streamed: the shell renders immediately; the first collection (parallel
// across streams) hydrates in. Subsequent views hit the warm sampler cache.
export const load: PageServerLoad = () => {
  return { lanes: loadLanes() };
};

// One lane mutation: the audit action id, the word its failure notice uses,
// and the call itself.
//
// The three were three copies of the same fifteen lines differing only in
// those three values, which is both the duplication CodeScene flags and how
// the delete action ended up with no audit row while alias had one. Table now,
// one skeleton below.
type LaneOp = {
  action: string;
  verb: string;
  run: (stream: string, consumer: string, f: FormData) => Promise<LaneMutationResult>;
};

// JetStream itself authorizes by NATS credential, not by console role, and the
// console holds one credential for every operator. So this gate is the only
// thing that separates a moderator from deleting a durable consumer.
function laneAction(op: LaneOp) {
  return async ({ request, locals }: RequestEvent) => {
    const admin = await requireRole({ locals }, 'lanes.mutate');
    if (!admin) return fail(403, { notice: 'forbidden' });
    if (DEMO) return { ok: false, notice: 'demo mode: lane mutations are disabled' };

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
      return fail(502, { notice: `${op.verb} failed: ${error}` });
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
