// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const flags: Record<string, boolean> = { '42': true, '7': false };
const services = await import('./services');
mock.module('#lib/server/services', () => ({
  ...services,
  accountState: async (userId: string) => ({ alpha: flags[userId] ?? false })
}));

const { assertAlphaRouteOpen } = await import('./alpha');

type Session = { user_id: string; delegate_of?: string };

function opens(route: string, session: Session | undefined, accountState?: object, pathname = '/'): Promise<number | null> {
  const event = { route: { id: route }, url: new URL(pathname, 'https://dash.example'), locals: { session, accountState } } as never;
  return assertAlphaRouteOpen(event).then(() => null, (refusal: { status: number }) => refusal.status);
}

test.each([
  { name: 'an alpha account on a new route', route: '/(app)/overlays/hub', session: { user_id: '42' }, want: null },
  { name: 'a free account on the hub', route: '/(app)/overlays/hub', session: { user_id: '7' }, want: 404 },
  { name: 'a free account on the editor', route: '/(app)/overlays/custom', session: { user_id: '7' }, want: 404 },
  { name: 'a free account on the asset endpoint', route: '/(app)/overlays/assets', session: { user_id: '7' }, want: 404 },
  { name: 'no session on a new route', route: '/(app)/overlays/feed', session: undefined, want: 404 },
  { name: 'a free account on the shipped overlays page', route: '/(app)/overlays', session: { user_id: '7' }, want: null },
  { name: 'a free account elsewhere', route: '/(app)/commands', session: { user_id: '7' }, want: null },
  { name: 'a delegate of an alpha owner', route: '/(app)/overlays/hub', session: { user_id: '7', delegate_of: '42' }, want: null },
  { name: 'a delegate of a free owner whose own account is alpha', route: '/(app)/overlays/hub', session: { user_id: '42', delegate_of: '7' }, want: 404 }
])('$name', async ({ route, session, want }) => {
  expect(await opens(route, session)).toBe(want);
});

test('a percent-encoded path cannot dodge the gate', async () => {
  expect(await opens('/(app)/overlays/hub', { user_id: '7' }, undefined, '/%6Fverlays/hub')).toBe(404);
});

test('the cached account state answers without another read', async () => {
  expect(await opens('/(app)/overlays/hub', { user_id: '7' }, { value: { alpha: true } })).toBeNull();
});
