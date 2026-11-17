// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { registerServerConfig } from '@bagel/kit/server/config';
import { staticText } from '../../../../../kit/lib/i18n/static';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
registerServerConfig({ cacheInvalidationPrefix: 'inv' });

let rewardReply: Record<string, unknown> | Error = {};
const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...realNats,
  publish: async () => {},
  rpcReply: async () => {
    if (rewardReply instanceof Error) throw rewardReply;
    return rewardReply;
  },
  rpc: async () => ({ present: true })
}));
mock.module('#lib/server/commands-store', () => ({
  listModules: async () => [{ name: 'govee', is_enabled: true, configs: { bindings: [] } }],
  upsertModule: async () => ({ modules: [], synced: true })
}));

const { actions } = await import('./+page.server');

function rewardForm(): FormData {
  const f = new FormData();
  for (const [name, value] of Object.entries({ device: 'AA', sku: 'H1', deviceName: 'Desk', title: 'Lights', cost: '100', color: '#ff0000', onRedeem: 'fulfill' })) {
    f.set(name, value);
  }
  return f;
}

function saveReward(): Promise<unknown> {
  const event = {
    request: new Request('https://dashboard.test/govee?/saveReward', { method: 'POST', body: rewardForm() }),
    locals: { session: { user_id: '7' }, locale: 'en' } as unknown as App.Locals
  };
  return (actions.saveReward as (e: typeof event) => Promise<unknown>)(event);
}

test.each([
  { name: 'a taken title', wire: { error: 'duplicate reward title', code: 'conflict' }, status: 409, flag: 'duplicateTitle', key: 'channelpoints.toastDuplicateTitle' },
  { name: 'a channel without affiliate', wire: { error: 'broadcaster not affiliate', code: 'forbidden', not_affiliate: true }, status: 403, flag: 'notAffiliate', key: 'channelpoints.toastNotAffiliate' }
])('saving a light reward over $name tells the streamer why', async ({ wire, status, flag, key }) => {
  rewardReply = wire;
  expect(await saveReward()).toMatchObject({ status, data: { ok: false, [flag]: true, error: staticText('en', key) } });
});

test.each([
  { name: 'an RPC timeout', wire: new Error('TIMEOUT') as Error | Record<string, unknown> },
  { name: 'an internal outgress refusal', wire: { error: 'twitch request failed', code: 'internal' } }
])('$name answers 502, not a client error', async ({ wire }) => {
  rewardReply = wire;
  expect(await saveReward()).toMatchObject({ status: 502, data: { ok: false, error: staticText('en', 'serverErrors.updateRetry') } });
});
