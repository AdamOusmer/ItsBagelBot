// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit({ dev: true });
Object.assign(process.env, { DEMO: '1', DEMO_PLAN: 'nonalpha' });

const { guardSession } = await import('./guard');

function status(route: string): Promise<number | null> {
  const event = { route: { id: route }, url: new URL('https://dash.example/x'), locals: {} } as never;
  const session = { sid: 's1', user_id: '42', iat: 1, expires_at: 2 } as never;
  return guardSession(event, session).then(() => null, (refusal: { status: number }) => refusal.status);
}

test.each([
  { name: 'an alpha route', route: '/(app)/overlays/hub', want: 404 },
  { name: 'the shipped overlays page', route: '/(app)/overlays', want: null },
  { name: 'another page', route: '/(app)/commands', want: null }
])('a preview account without alpha on $name', async ({ route, want }) => {
  expect(await status(route)).toBe(want);
});
