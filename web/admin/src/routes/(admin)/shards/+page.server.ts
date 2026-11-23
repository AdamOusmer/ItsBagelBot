// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import { fail } from '@sveltejs/kit';
import { dev } from '$app/env';
import type { ShardSnapshot } from '@bagel/kit';
import type { Locale } from '@bagel/kit/i18n';
import { shardScale, shardAutoscale, trialList } from '#lib/server/services';
import { allows, requireRole } from '#lib/server/access';
import { audit } from '#lib/server/audit';
import { shardPageReads } from '#lib/server/shard-page-reads';
import { adminText, okReply } from '#lib/server/admin-action';
import { readShardSnapshot } from '#lib/components/shards/fleet-read.server';

const DEMO = dev && process.env.DEMO === '1';

export const load = (async ({ parent }) => {
  const { role } = await parent();
  const canViewTrials = allows(role, 'trials.manage');
  return {
    ...shardPageReads(readShardSnapshot, canViewTrials ? readTrialSnapshot : async () => null),
    canViewTrials
  };
}) satisfies PageServerLoad;

function readTrialSnapshot() {
  return DEMO
    ? import('#lib/server/demo-data').then(({ sampleTrials }) => sampleTrials)
    : trialList();
}

function parseCount(form: FormData): number | null {
  const value = String(form.get('count') ?? '').trim();
  if (!/^\d+$/.test(value)) return null;
  const count = Number(value);
  return Number.isSafeInteger(count) && count > 0 ? count : null;
}

function parseEnabled(form: FormData): boolean | null {
  const enabled = form.get('enabled');
  if (enabled === 'true') return true;
  return enabled === 'false' ? false : null;
}

type ShardMutation<T> = {
  action: 'shard_scale' | 'shard_autoscale';
  parse: (form: FormData) => T | null;
  invalidKey: string;
  update: (value: T) => Promise<ShardSnapshot>;
  failureKey: string;
  notice: (locale: Locale, snapshot: ShardSnapshot) => string;
  demoNotice: (locale: Locale, value: T) => string;
};

function changeShardSettings<T extends boolean | number>(mutation: ShardMutation<T>) {
  return async ({ request, locals }: RequestEvent) => {
    const { locale } = locals;
    // Ingress trusts broker callers, so these actions must enforce the staff role.
    const admin = await requireRole({ locals }, 'shards.scale');
    if (!admin) return fail(403, { error: adminText(locale, 'admin.action.forbidden') });

    const value = mutation.parse(await request.formData());
    if (value === null) return fail(400, { error: adminText(locale, mutation.invalidKey) });
    if (DEMO) {
      const snapshot = await readShardSnapshot();
      return { ...okReply(mutation.demoNotice(locale, value)), snapshot };
    }

    const entry = { action: mutation.action, target: 'ingress', detail: String(value) };
    try {
      const snapshot = await mutation.update(value);
      audit(admin, { ...entry, ok: true });
      return { ...okReply(mutation.notice(locale, snapshot)), snapshot };
    } catch (error) {
      audit(admin, { ...entry, ok: false, error: (error as Error).message });
      return fail(500, { error: adminText(locale, mutation.failureKey) });
    }
  };
}

export const actions = {
  scale: changeShardSettings({
    action: 'shard_scale',
    parse: parseCount,
    invalidKey: 'admin.action.countPositive',
    update: shardScale,
    failureKey: 'admin.shards.scaleFailed',
    notice: (locale, snapshot) => adminText(locale, 'admin.shards.scaled', { count: snapshot.desired_count }),
    demoNotice: (locale, count) => adminText(locale, 'admin.shards.scaleDemo', { count })
  }),
  autoscale: changeShardSettings({
    action: 'shard_autoscale',
    parse: parseEnabled,
    invalidKey: 'admin.shards.autoscaleFailed',
    update: shardAutoscale,
    failureKey: 'admin.shards.autoscaleFailed',
    notice: (locale, snapshot) => adminText(locale, snapshot.autoscale
      ? 'admin.shards.autoscaleEnabled'
      : 'admin.shards.autoscaleDisabled'),
    demoNotice: (locale, enabled) => adminText(locale, enabled
      ? 'admin.shards.autoscaleOnDemo'
      : 'admin.shards.autoscaleOffDemo')
  })
} satisfies Actions;
