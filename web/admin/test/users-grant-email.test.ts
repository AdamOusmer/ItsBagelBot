// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from './sveltekit';
import type { GrantEmailOutcome, UserRef, VoiceUsageChange } from '../src/lib/server/services';

stubSvelteKit();

const endDay = new Date(Date.now() + 30 * 864e5).toISOString().slice(0, 10);
const expiresAt = `${endDay}T23:59:59.999Z`;
const storedExpiry = new Date(Date.now() + 31 * 864e5).toISOString();
const grantedUser = { id: 21, username: 'crumbs', status: 'paid', is_active: true, banned: false, subscription_expires_at: storedExpiry };
const ref: UserRef = { actorId: '10', userId: '21' };
const admin = { id: ref.actorId, login: 'mod', display_name: 'Mod', role: 'admin' };
const voiceUser = {
  ...grantedUser,
  voice_allowance: { plan: 'premium', used_tokens: 0, limit_tokens: 90000, period: '2026-11', resets_at: '2026-12-01T00:00:00Z' }
};

let emailReply: () => Promise<GrantEmailOutcome> = async () => 'sent';
let allowed = true;

const userSetStatus = mock(async (_ref: UserRef, _status: string, _expiresAt?: string) => grantedUser);
const premiumGrantEmail = mock((_ref: UserRef, _expiresAt: string) => emailReply());
const userSetVoiceUsage = mock(async (_ref: UserRef, change: VoiceUsageChange) => ({
  ...voiceUser,
  voice_allowance: { ...voiceUser.voice_allowance, used_tokens: (change.used_percent ?? 0) * 900 }
}));
const requireRole = mock(async (_event: unknown, _key: string) => (allowed ? admin : null));
const audit = mock(() => {});

const services = await import('../src/lib/server/services');
mock.module('#lib/server/services', () => ({ ...services, userSetStatus, premiumGrantEmail, userSetVoiceUsage }));
mock.module('#lib/server/access', () => ({ requireRole }));
mock.module('#lib/server/audit', () => ({ audit }));
mock.module('virtual:bagel-i18n', () => ({ urls: {}, loaders: {}, sliced: false }));
mock.module('@bagel/kit/i18n', () => ({
  translate: (_locale: string, key: string, params?: { until?: string }) => key + (params?.until ?? '')
}));

const { actions } = await import('../src/routes/(admin)/users/+page.server');

function submit(action: 'setStatus' | 'setVoiceUsage', fields: Record<string, string>) {
  const body = new FormData();
  for (const [key, value] of Object.entries(fields)) body.set(key, value);
  const request = new Request(`https://admin.example/users?/${action}`, { method: 'POST', body });
  return actions[action]({ request, locals: { locale: 'en' } } as never);
}

beforeEach(() => {
  emailReply = async () => 'sent';
  allowed = true;
  userSetStatus.mockClear();
  premiumGrantEmail.mockClear();
  userSetVoiceUsage.mockClear();
  requireRole.mockClear();
  audit.mockClear();
});

const paid = { user_id: '21', status: 'paid', expires_at: endDay };
const notify = { ...paid, notify_email: 'on' };

describe('paid grant email', () => {
  test.each([
    { label: 'stays silent unless asked', fields: paid, reply: async () => 'sent', calls: 0, suffix: '' },
    { label: 'sends with the stored end date', fields: notify, reply: async () => 'sent', calls: 1, suffix: ' · admin.users.grantEmail.sent' },
    { label: 'reports a missing contact email', fields: notify, reply: async () => 'no_email', calls: 1, suffix: ' · admin.users.grantEmail.noAddress' },
    {
      label: 'keeps the grant when the email fails',
      fields: notify,
      reply: async () => {
        throw new Error('unavailable');
      },
      calls: 1,
      suffix: ' · admin.users.grantEmail.failed'
    }
  ] satisfies { label: string; fields: Record<string, string>; reply: () => Promise<GrantEmailOutcome>; calls: number; suffix: string }[])('$label', async ({ fields, reply, calls, suffix }) => {
    emailReply = reply;
    const result = await submit('setStatus', fields);
    expect(result).toEqual({
      action: { ok: true, notice: `admin.users.actionResults.statusSetUntil until ${storedExpiry.slice(0, 10)}${suffix}` },
      lookup: { user: grantedUser }
    });
    expect(userSetStatus.mock.calls).toEqual([[ref, 'paid', expiresAt]]);
    expect(premiumGrantEmail.mock.calls).toEqual(calls ? [[ref, storedExpiry]] : []);
  });
});

describe('Premium voice usage', () => {
  const validChanges: { fields: Record<string, string>; change: VoiceUsageChange; notice: string; used: number }[] = [
    { fields: { reset: 'true' }, change: { reset: true }, notice: 'resetDone', used: 0 },
    { fields: { used_percent: '0' }, change: { used_percent: 0 }, notice: 'setDone', used: 0 },
    { fields: { used_percent: '75' }, change: { used_percent: 75 }, notice: 'setDone', used: 67500 },
    { fields: { used_percent: '100' }, change: { used_percent: 100 }, notice: 'setDone', used: 90000 }
  ];
  test.each(validChanges)(
    'submits $fields and returns the updated allowance', async ({ fields, change, notice, used }) => {
      const result = await submit('setVoiceUsage', { user_id: ref.userId, ...fields });
      expect(result).toEqual({
        action: { ok: true, notice: `admin.users.voice.${notice}` },
        lookup: { user: { ...voiceUser, voice_allowance: { ...voiceUser.voice_allowance, used_tokens: used } } }
      });
      expect(userSetVoiceUsage.mock.calls).toEqual([[ref, change]]);
      expect(audit.mock.calls).toEqual([]);
    }
  );

  const invalidChanges: Record<string, string>[] = [
    {},
    { used_percent: '-1' },
    { used_percent: '101' },
    { used_percent: '25.5' },
    { reset: 'true', used_percent: '25' }
  ];
  test.each(invalidChanges)('rejects invalid input %j before calling Users', async (fields) => {
    expect(await submit('setVoiceUsage', { user_id: ref.userId, ...fields })).toMatchObject({
      status: 400, data: { error: 'admin.users.voice.invalidChange' }
    });
    expect(userSetVoiceUsage.mock.calls).toEqual([]);
  });

  test('requires the Premium-grant permission', async () => {
    allowed = false;
    const result = await submit('setVoiceUsage', { user_id: ref.userId, reset: 'true' });
    expect(result).toMatchObject({ status: 403, data: { error: 'admin.action.forbidden' } });
    expect(requireRole.mock.calls.map(([, key]) => key)).toEqual(['users.grant']);
    expect(userSetVoiceUsage.mock.calls).toEqual([]);
  });
});
