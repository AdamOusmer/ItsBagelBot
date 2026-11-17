// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';
import { captureAnalytics } from '../../../test/analytics';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

const services = await import('../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  accountState: async () => ({ active: true, status: 'free', lifecycle: 'active' }),
  notificationsForUser: async () => ({ unreadCount: 0, notifications: [] }),
  delegationAccess: async () => []
}));

const { scopes } = await import('../../lib/server/oauth');
const { load } = await import('./+layout.server');
const recorded = captureAnalytics();

async function landing(session: Record<string, unknown>): Promise<string | null> {
  const event = {
    locals: { session: { user_id: '1001', ...session }, locale: 'en' },
    url: new URL('https://dashboard.itsbagelbot.com/commands?tab=1'),
    route: { id: '/(app)/commands' },
    request: new Request('https://dashboard.itsbagelbot.com/commands')
  };
  try {
    await load(event as never);
    return null;
  } catch (e) {
    return (e as { location?: string }).location ?? null;
  }
}

const granted = () => scopes().filter((s) => s !== 'openid');

describe('dashboard layout scope gate', () => {
  test('an owner missing a scope is sent to Twitch and returns to the same page', async () => {
    expect(await landing({ scopes: granted().slice(1) })).toBe('/auth/login?reauth=1&next=%2Fcommands%3Ftab%3D1');
  });

  test('an owner with every scope stays', async () => {
    expect(await landing({ scopes: granted() })).toBeNull();
  });

  test('a delegate is never sent to consent for their own account', async () => {
    expect(await landing({ scopes: [], delegate_of: '2002', sections: [] })).toBeNull();
  });

  test('an impersonating admin is never sent to consent', async () => {
    expect(await landing({ scopes: [], impersonator_id: '3003' })).toBeNull();
  });
});

test.each([
  { name: 'unaccepted owner on a feature page', onboarded: false, route: '/(app)/commands', session: {}, landing: '/welcome' },
  { name: 'unaccepted owner on the overview', onboarded: false, route: '/(app)', session: {}, landing: '/welcome' },
  { name: 'unaccepted owner in settings', onboarded: false, route: '/(app)/settings', session: {}, landing: null },
  { name: 'accepted owner', onboarded: true, route: '/(app)/commands', session: {}, landing: null },
  { name: 'unaccepted delegate', onboarded: false, route: '/(app)/commands', session: { delegate_of: '2002', sections: ['commands'] }, landing: null },
  { name: 'unaccepted impersonated owner', onboarded: false, route: '/(app)/commands', session: { impersonator_id: '3003' }, landing: null },
  { name: 'unreadable account state', onboarded: null, route: '/(app)/commands', session: {}, landing: null }
])('consent gate: $name', async ({ onboarded, route, session, landing }) => {
  const accountState = onboarded === null ? undefined : { value: { active: true, status: 'free', onboarded } };
  const event = {
    locals: { session: { user_id: '1001', scopes: granted(), ...session }, locale: 'en', accountState },
    url: new URL('https://dashboard.itsbagelbot.com/commands'),
    route: { id: route },
    request: new Request('https://dashboard.itsbagelbot.com/commands')
  };
  const location = await Promise.resolve(load(event as never)).then(() => null, (e: { location?: string }) => e.location ?? null);
  expect(location).toBe(landing);
});

test.each([
  { route: '/(app)', method: 'GET', delegate: false, field: 'dashboard.views|overview|owner' },
  { route: '/(app)/settings/import', method: 'GET', delegate: true, field: 'dashboard.views|settings|delegate' },
  { route: '/(app)/modules/[id]', method: 'GET', delegate: false, field: 'dashboard.views|modules|owner' },
  { route: '/(app)/unknown', method: 'GET', delegate: false, field: null },
  { route: null, method: 'GET', delegate: false, field: null },
  { route: '/(app)/commands', method: 'POST', delegate: false, field: null }
])('counts one page view for $method $route', async ({ route, method, delegate, field }) => {
  await load({
    locals: { session: { user_id: '1001', scopes: granted(), delegate_of: delegate ? '2002' : undefined }, locale: 'en' },
    url: new URL('https://dashboard.itsbagelbot.com/settings/import?private=value'),
    route: { id: route },
    request: new Request('https://dashboard.itsbagelbot.com/settings/import', { method })
  } as never);
  expect(await recorded()).toEqual(field ? [[field, 1]] : []);
});
