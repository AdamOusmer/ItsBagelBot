// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

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

async function landing(session: Record<string, unknown>): Promise<string | null> {
  const event = {
    locals: { session: { user_id: '1001', ...session }, locale: 'en' },
    url: new URL('https://dashboard.itsbagelbot.com/commands?tab=1')
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
