// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The setCommandsPage action (docs/specs/commands-page-toggle.md §5.2, §9):
// owner-only (delegate and admin "view as" both refused), writes the opposite
// of `enabled` as commands_page_hidden, then best-effort purges the edge for
// the account's canonical login. A failed purge surfaces as edgeDelayed
// rather than failing the request the write already committed.
import { describe, expect, mock, test } from 'bun:test';

let setCommandsPageCalls: [string, boolean][] = [];
let purgeUrls: string[][] = [];
let purgeReply = true;
let accountUsername = 'streamerlogin';

mock.module('$app/environment', () => ({ dev: false }));
mock.module('$env/dynamic/private', () => ({ env: {} }));
mock.module('@bagel/kit/i18n', () => ({
  isLocale: (v: unknown) => v === 'en' || v === 'fr',
  DEFAULT_LOCALE: 'en'
}));
mock.module('@bagel/kit', () => ({
  KEY_VALUE_MAX: 4096,
  slugifyName: (s: string) => s.toLowerCase(),
  GRANTABLE_SECTIONS: ['commands', 'modules']
}));
mock.module('$lib/server/session', () => ({
  ACCOUNT_DELETED_COOKIE: 'bb_deleted',
  COOKIE: 'bagel_session',
  SESSION_TTL_SECONDS: 3600
}));
mock.module('@bagel/kit/server/session-revocation', () => ({
  revokeAllForUser: async () => {},
  revokeSession: async () => {}
}));
mock.module('$lib/server/fetches-store', () => ({
  deleteFetchKey: async () => {},
  listFetches: async () => ({ defs: [], keys: [] }),
  setFetchKey: async () => 'abcd'
}));
mock.module('$lib/server/edge-purge', () => ({
  purgeEdge: async (urls: string[]) => {
    purgeUrls.push(urls);
    return purgeReply;
  }
}));
mock.module('$lib/server/services', () => ({
  delegationList: async () => [],
  delegationAccess: async () => [],
  delegationCreate: async () => '',
  delegationUpdate: async () => {},
  delegationOptOut: async () => {},
  delegationRevoke: async () => {},
  deleteSelf: async () => {},
  publishEventSub: async () => {},
  auditDashboardImpersonation: () => {},
  notificationsForUser: async () => ({ notifications: [] }),
  notificationMarkRead: async () => {},
  notificationMarkPeeked: async () => {},
  userLocale: async () => 'en',
  userCommandsPage: async () => true,
  setCommandsPage: async (userId: string, hidden: boolean) => {
    setCommandsPageCalls.push([userId, hidden]);
  },
  accountState: async () => ({
    active: true,
    status: 'free',
    onboarded: true,
    creatorCode: null,
    username: accountUsername,
    displayName: ''
  })
}));

const { actions } = await import('./+page.server');

function formRequest(fields: Record<string, string>): Request {
  const body = new URLSearchParams(fields);
  return new Request('https://dashboard.itsbagelbot.com/settings?/setCommandsPage', {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: body.toString()
  });
}

type Session = { user_id: string; delegate_of?: string; impersonator_id?: string };

function event(session: Session | null, fields: Record<string, string>) {
  return { request: formRequest(fields), locals: { session } } as never;
}

describe('setCommandsPage action', () => {
  test('a delegate session is refused', async () => {
    setCommandsPageCalls = [];
    const result = (await actions.setCommandsPage(event({ user_id: '1', delegate_of: '2' }, { enabled: 'on' }))) as {
      status: number;
    };
    expect(result.status).toBe(403);
    expect(setCommandsPageCalls).toEqual([]);
  });

  test('an admin "view as" session is refused', async () => {
    setCommandsPageCalls = [];
    const result = (await actions.setCommandsPage(event({ user_id: '1', impersonator_id: '9' }, { enabled: 'on' }))) as {
      status: number;
    };
    expect(result.status).toBe(403);
  });

  test('owner enabled=on writes hidden=false then purges the canonical URL', async () => {
    setCommandsPageCalls = [];
    purgeUrls = [];
    purgeReply = true;
    accountUsername = 'StreamerLogin';

    const result = await actions.setCommandsPage(event({ user_id: '7' }, { enabled: 'on' }));

    expect(setCommandsPageCalls).toEqual([['7', false]]);
    expect(purgeUrls).toEqual([['https://commands.itsbagelbot.com/user/streamerlogin']]);
    expect(result).toEqual({ ok: true, action: 'commands_page', edgeDelayed: false });
  });

  test('a failed purge surfaces edgeDelayed: true without failing the request', async () => {
    setCommandsPageCalls = [];
    purgeReply = false;

    const result = await actions.setCommandsPage(event({ user_id: '7' }, { enabled: 'on' }));

    expect(result).toEqual({ ok: true, action: 'commands_page', edgeDelayed: true });
  });

  test('enabled absent (unchecked switch) writes hidden=true', async () => {
    setCommandsPageCalls = [];
    purgeReply = true;

    await actions.setCommandsPage(event({ user_id: '7' }, {}));

    expect(setCommandsPageCalls).toEqual([['7', true]]);
  });
});
