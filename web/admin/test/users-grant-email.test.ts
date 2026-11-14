// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from './sveltekit';

stubSvelteKit();

const endDay = new Date(Date.now() + 30 * 864e5).toISOString().slice(0, 10);
const expiresAt = `${endDay}T23:59:59.999Z`;
const grantedUser = { id: 21, username: 'crumbs', status: 'paid', is_active: true, banned: false, subscription_expires_at: expiresAt };

let emailReply: () => Promise<string> = async () => 'sent';

const userSetStatus = mock(async () => grantedUser);
const premiumGrantEmail = mock((..._args: unknown[]) => emailReply());

const services = await import('../src/lib/server/services');
mock.module('#lib/server/services', () => ({ ...services, userSetStatus, premiumGrantEmail }));
mock.module('#lib/server/access', () => ({
  requireRole: async () => ({ id: '10', login: 'mod', display_name: 'Mod', role: 'admin' })
}));
mock.module('#lib/server/audit', () => ({ audit() {} }));
mock.module('virtual:bagel-i18n', () => ({ urls: {}, loaders: {}, sliced: false }));
mock.module('@bagel/kit/i18n', () => ({
  translate: (_locale: string, key: string, params?: { until?: string }) => key + (params?.until ?? '')
}));

const { actions } = await import('../src/routes/(admin)/users/+page.server');

function setStatus(fields: Record<string, string>) {
  const body = new FormData();
  for (const [key, value] of Object.entries(fields)) body.set(key, value);
  const request = new Request('https://admin.example/users?/setStatus', { method: 'POST', body });
  return actions.setStatus({ request, locals: { locale: 'en' } } as never) as Promise<{ action: { ok: boolean; notice: string } }>;
}

beforeEach(() => {
  emailReply = async () => 'sent';
  userSetStatus.mockClear();
  premiumGrantEmail.mockClear();
});

const paid = { user_id: '21', status: 'paid', expires_at: endDay };
const notify = { ...paid, notify_email: 'on' };

describe('paid grant email', () => {
  test.each([
    { label: 'stays silent unless asked', fields: paid, reply: async () => 'sent', calls: 0, suffix: '' },
    { label: 'sends with the stored end date', fields: notify, reply: async () => 'sent', calls: 1, suffix: ' · admin.users.grantEmailSent' },
    { label: 'reports a missing contact email', fields: notify, reply: async () => 'no_email', calls: 1, suffix: ' · admin.users.grantEmailNoAddress' },
    {
      label: 'keeps the grant when the email fails',
      fields: notify,
      reply: async () => {
        throw new Error('unavailable');
      },
      calls: 1,
      suffix: ' · admin.users.grantEmailFailed'
    }
  ])('$label', async ({ fields, reply, calls, suffix }) => {
    emailReply = reply;
    const result = await setStatus(fields);
    expect(result.action).toEqual({ ok: true, notice: `admin.users.statusSetUntil until ${endDay}${suffix}` });
    expect(premiumGrantEmail.mock.calls).toEqual(calls ? [[{ actorId: '10', userId: '21' }, expiresAt]] : []);
  });
});
