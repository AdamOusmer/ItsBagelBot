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

function opens(path: string, session: Session | undefined, accountState?: object): Promise<number | null> {
  const event = { url: new URL(`https://dash.example${path}`), locals: { session, accountState } } as never;
  return assertAlphaRouteOpen(event).then(() => null, (refusal: { status: number }) => refusal.status);
}

test.each([
  { name: 'an alpha account on a new route', path: '/overlays/hub', session: { user_id: '42' }, want: null },
  { name: 'a free account on the hub', path: '/overlays/hub', session: { user_id: '7' }, want: 404 },
  { name: 'a free account on the editor', path: '/overlays/custom', session: { user_id: '7' }, want: 404 },
  { name: 'a free account on the asset endpoint', path: '/overlays/assets', session: { user_id: '7' }, want: 404 },
  { name: 'no session on a new route', path: '/overlays/feed', session: undefined, want: 404 },
  { name: 'a free account on the shipped overlays page', path: '/overlays', session: { user_id: '7' }, want: null },
  { name: 'a free account elsewhere', path: '/commands', session: { user_id: '7' }, want: null },
  { name: 'a delegate of an alpha owner', path: '/overlays/hub', session: { user_id: '7', delegate_of: '42' }, want: null },
  { name: 'a delegate of a free owner whose own account is alpha', path: '/overlays/hub', session: { user_id: '42', delegate_of: '7' }, want: 404 }
])('$name', async ({ path, session, want }) => {
  expect(await opens(path, session)).toBe(want);
});

test('the cached account state answers without another read', async () => {
  expect(await opens('/overlays/hub', { user_id: '7' }, { value: { alpha: true } })).toBeNull();
});
