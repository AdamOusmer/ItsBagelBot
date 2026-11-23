// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { stubSvelteKit } from '../../../test/sveltekit';
import { captureAnalytics } from '../../../test/analytics';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

const services = await import('../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  accountState: async () => { throw new Error('state unavailable'); },
  notificationsForUser: async () => ({ unreadCount: 0, notifications: [] }),
  delegationAccess: async () => []
}));

const { scopes } = await import('../../lib/server/oauth');
const { load } = await import('./+layout.server');
const recorded = captureAnalytics();

const granted = () => scopes().filter((s) => s !== 'openid');

type GateCase = {
  name: string;
  path?: string;
  terms?: string;
  session?: Record<string, unknown>;
  missingScope?: boolean;
  method?: 'POST';
  ghost?: boolean;
  readFailed?: boolean;
  landing: string | null;
};

const gateCases: GateCase[] = [
  { name: 'scope gap precedes terms', missingScope: true, terms: '', landing: '/auth/login?reauth=1&next=%2Fcommands%3Ftab%3D1' },
  { name: 'state from an older Users pod omits the terms version', landing: null },
  { name: 'legacy acceptance needs current terms', terms: '', landing: '/consent?next=%2Fcommands%3Ftab%3D1' },
  { name: 'older terms allow opening settings', path: '/settings', terms: '2025-12', landing: null },
  { name: 'older terms allow submitting a settings withdrawal', path: '/settings?/marketing', method: 'POST', terms: '2025-12', landing: null },
  { name: 'settings still checks Twitch scopes', path: '/settings', missingScope: true, terms: '2025-12', landing: '/auth/login?reauth=1&next=%2Fsettings' },
  { name: 'current terms', terms: TERMS_VERSION, landing: null },
  { name: 'newer terms during a rollback', terms: '9999-12', landing: null },
  { name: 'unsafe return path', path: '//example.test', terms: '', landing: '/consent?next=%2F' },
  { name: 'delegate with a scope gap and older terms', session: { delegate_of: '2002' }, missingScope: true, terms: '', landing: null },
  { name: 'impersonation with a scope gap and older terms', session: { impersonator_id: '3003' }, missingScope: true, terms: '', landing: null },
  { name: 'an unreadable account state fails open', readFailed: true, landing: null },
  { name: 'a state marker without a value fails open', ghost: true, landing: null },
  {
    name: 'a regular billing visit still needs terms', path: '/billing?subscribe=1', terms: '',
    landing: '/consent?next=%2Fbilling%3Fsubscribe%3D1'
  },
  ...['/billing?checkout=complete', '/billing?checkout=cancelled'].map((path) => ({
    name: `exempt path ${path}`, path, missingScope: true, terms: '', landing: null
  }))
];

function gateState(row: GateCase) {
  if (row.ghost) return { ghost: true };
  if (row.readFailed) return undefined;
  return { value: { active: true, status: 'free', onboarded: true, termsVersion: row.terms } };
}

test.each(gateCases)('dashboard gates: $name', async (row) => {
  const url = new URL(`https://dashboard.itsbagelbot.com${row.path ?? '/commands?tab=1'}`);
  const event = {
    locals: { session: { user_id: '1001', scopes: row.missingScope ? [] : granted(), ...row.session }, locale: 'en', accountState: gateState(row) },
    url,
    route: { id: `/(app)${url.pathname}` },
    request: new Request(url, { method: row.method })
  };
  const landing = await Promise.resolve(load(event as never)).then(
    () => null,
    (e: { location?: string }) => e.location
  );
  expect(landing).toBe(row.landing);
});

test.each([
  { route: '/(app)', method: 'GET', delegate: false, field: 'dashboard.views|overview|owner' },
  { route: '/(app)/settings/import', method: 'GET', delegate: true, field: 'dashboard.views|settings|delegate' },
  { route: '/(app)/modules/[id]', method: 'GET', delegate: false, field: 'dashboard.views|modules|owner' },
  { route: '/(app)/voice', method: 'GET', delegate: false, field: 'dashboard.views|voice|owner' },
  { route: '/(app)/voice', method: 'GET', delegate: true, field: 'dashboard.views|voice|delegate' },
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
