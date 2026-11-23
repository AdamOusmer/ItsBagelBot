// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const { MODULE_CATALOG } = await import('@bagel/kit');
const { assertBetaRouteOpen } = await import('./module-gate');

const beta = MODULE_CATALOG.find((def) => def.beta && def.href && !def.section);

function outcome(routeId: string | null, pathname: string): Promise<number | null> {
  const event = {
    route: { id: routeId },
    url: new URL(pathname, 'https://dash.example'),
    locals: { session: { user_id: '7' }, accountState: { value: { status: 'free' } } }
  } as never;
  return assertBetaRouteOpen(event).then(() => null, (refusal: { status: number }) => refusal.status);
}

test.each([
  { name: 'the route id of a beta module', routeId: `/(app)${beta?.href}`, pathname: beta?.href ?? '/', want: 303 },
  { name: 'a percent-encoded path of a beta module', routeId: `/(app)${beta?.href}`, pathname: `/%${(beta?.href ?? '/x').charCodeAt(1).toString(16)}${(beta?.href ?? '/x').slice(2)}`, want: 303 },
  { name: 'a route outside the beta modules', routeId: '/(app)/commands', pathname: '/commands', want: null },
  { name: 'no matched route', routeId: null, pathname: beta?.href ?? '/', want: null }
])('a free account on $name', async ({ routeId, pathname, want }) => {
  expect(beta).toBeDefined();
  expect(await outcome(routeId, pathname)).toBe(want);
});
