// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, spyOn, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
mock.module('#lib/server/edge-purge', () => ({
  schedulePurgeChannel: () => {},
  scheduleLeaderboardPurge: () => {}
}));

const bus = await import('@bagel/kit/server/nats');
const assets = await import('./assets/+server');
const feed = await import('./feed/+server');
const imports = await import('./imports/+server');
const hub = await import('./hub/+page.server');
const powered = await import('./powered/+page.server');
const custom = await import('./custom/+page.server');

type Handler = (event: never) => unknown;

const handlers: Record<string, Handler> = {
  'assets GET': assets.GET as Handler,
  'assets DELETE': assets.DELETE as Handler,
  'assets POST': assets.POST as Handler,
  'assets PUT': assets.PUT as Handler,
  'imports POST': imports.POST as Handler,
  'imports PUT': imports.PUT as Handler,
  'feed POST': feed.POST as Handler,
  'hub load': hub.load as Handler,
  'hub add': hub.actions.add as Handler,
  'hub rotate': hub.actions.rotate as Handler,
  'hub delete': hub.actions.delete as Handler,
  'powered load': powered.load as Handler,
  'powered save': powered.actions.save as Handler,
  'powered rotate': powered.actions.rotate as Handler,
  'custom load': custom.load as Handler,
  'custom save': custom.actions.save as Handler
};

function event(alpha: boolean) {
  return {
    locals: { session: { user_id: '42' }, locale: 'en', accountState: { value: { alpha } } },
    request: new Request('https://dashboard.test/overlays/x', { method: 'POST', body: '{}' }),
    url: new URL('https://dashboard.test/overlays/x'),
    setHeaders: () => {}
  } as never;
}

test.each(Object.keys(handlers))('%s refuses an account without alpha before any service call', async (name) => {
  const rpc = spyOn(bus, 'rpc');
  const refusal = await Promise.resolve(handlers[name](event(false))).then(
    () => null,
    (thrown: { status: number }) => thrown.status
  );
  expect([refusal, rpc.mock.calls.length]).toEqual([404, 0]);
  rpc.mockRestore();
});
