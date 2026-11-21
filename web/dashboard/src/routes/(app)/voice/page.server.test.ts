// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { ALERT_VOICE_CONFIG_DEFAULTS, TTS_COMMAND_CONFIG_DEFAULTS } from '@bagel/kit/voice-plan';
import type { VoiceAllowance } from '#lib/server/services/voice';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
const { auditDashboardImpersonation, auditImpersonation } = await import('#lib/server/services/audit');
const audits: [unknown, string][] = [];
mock.module('#lib/server/services/audit', () => ({
  auditImpersonation,
  auditDashboardImpersonation: (...args: Parameters<typeof auditDashboardImpersonation>) => {
    audits.push([args[0], args[1]]);
    auditDashboardImpersonation(...args);
  }
}));
mock.module('#lib/server/edge-purge', () => ({
  schedulePurgeChannel: () => {},
  scheduleLeaderboardPurge: () => {}
}));

const bus = await import('@bagel/kit/server/nats');
const { READ_TIMEOUT_MS, WRITE_TIMEOUT_MS } = await import('@bagel/kit/server/service');
const { SUB } = await import('#lib/server/services/subjects');
const { actions, load } = await import('./+page.server');

const owner = { user_id: '42' };
const delegate = { user_id: '95', delegate_of: '42', sections: ['modules'] };
const viewAs = { user_id: '42', impersonator_id: '97' };
const actor = { actor_id: '42', broadcaster_user_id: '42' };
const allowance: VoiceAllowance = {
  plan: 'premium', period: '2026-11', limit_tokens: 90_000, used_tokens: 20_025,
  remaining_tokens: 69_975, resets_at: '2026-12-01T00:00:00Z', enabled: true, voice: 'Puck', max_seconds: 45, reason: 'ok'
};
const free: VoiceAllowance = {
  ...allowance, plan: 'free', limit_tokens: 0, used_tokens: 0, remaining_tokens: 0,
  enabled: false, voice: 'Kore', max_seconds: 120, reason: 'no_plan'
};
const voiceUrl = 'https://dashboard.test/voice';
const overlayUrl = 'https://overlay.itsbagelbot.com/tts#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';

afterEach(() => mock.restore());

const alertRow = {
  name: 'alerts', is_enabled: false, revision: 9,
  configs: { cheerTtsStyle: 'calm', followMessage: 'Chat only', followTts: 'true', cheerTtsMessage: 'Legacy' }
};
const alertFields: Record<string, string> = {
  ...ALERT_VOICE_CONFIG_DEFAULTS, subTts: 'false', cheerTtsStyle: 'calm', subTtsVoice: 'Charon'
};
type RpcCall = Parameters<typeof bus.rpc>;
const usersRead: RpcCall = [`${SUB.dashboard}.voice_get`, actor, READ_TIMEOUT_MS];
const modulesRead: RpcCall = [`${SUB.modules}.list`, { user_id: '42' }, 2000];

test.each([
  { name: 'paid', session: owner, reply: allowance, readOnly: false },
  { name: 'giveaway', session: owner, reply: { ...allowance, plan: 'giveaway' }, readOnly: false },
  { name: 'admin grant', session: owner, reply: { ...allowance, plan: 'admin_grant' }, readOnly: false },
  { name: 'VIP', session: owner, reply: { ...allowance, plan: 'vip' }, readOnly: false },
  { name: 'free', session: owner, reply: free, readOnly: false },
  { name: 'inactive', session: owner, reply: { ...free, reason: 'inactive' }, readOnly: false },
  { name: 'banned', session: owner, reply: { ...free, reason: 'banned' }, readOnly: false },
  { name: 'delegate', session: delegate, reply: null, readOnly: true },
  { name: 'view-as', session: viewAs, reply: allowance, readOnly: false }
])('loads voice data for $name', async ({ session, reply, readOnly }) => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(reply).mockResolvedValueOnce({ modules: [alertRow] });
  const alerts = reply?.reason === 'ok'
    ? { config: { ...ALERT_VOICE_CONFIG_DEFAULTS, cheerTtsStyle: 'calm' }, revision: 9 }
    : null;
  const command = alerts ? { config: { ...TTS_COMMAND_CONFIG_DEFAULTS }, enabled: false, revision: 0 } : null;

  expect(await load({ locals: { session, locale: 'en' } } as never)).toEqual({
    readOnly, allowance: reply, locked: reply?.reason === 'no_plan', alerts, command
  });
  expect(rpc.mock.calls).toEqual([...(readOnly ? [] : [usersRead]), ...(alerts ? [modulesRead] : [])]);
});

test('an unavailable allowance degrades without inventing a free plan', async () => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await load({ locals: { session: owner, locale: 'en' } } as never)).toEqual({
    readOnly: false, allowance: null, locked: false, alerts: null, command: null
  });
});

function save(fields: Record<string, string> = {}, session: typeof owner | null = owner) {
  const body = new URLSearchParams({
    ...alertFields, enabled: 'true', voice: 'Kore', max_seconds: '120', expected_rev: '9', ...fields
  });
  return actions.save({
    request: new Request(`${voiceUrl}?/save`, { method: 'POST', body }),
    locals: { session, locale: 'en' }
  } as never);
}

function savePartial(fields: object) {
  return actions.save({
    request: new Request(`${voiceUrl}?/save`, { method: 'POST', body: new URLSearchParams(fields as Record<string, string>) }),
    locals: { session: owner, locale: 'en' }
  } as never);
}

function mockSaveReplies(moduleReply: { conflict?: boolean; rev: number; modules?: unknown[] }) {
  return spyOn(bus, 'rpc').mockResolvedValueOnce(allowance).mockResolvedValueOnce({ modules: [alertRow] })
    .mockResolvedValueOnce(moduleReply);
}

test.each([
  { voice: 'Puck', enabled: true, maxSeconds: 5 },
  { voice: 'Kore', enabled: false, maxSeconds: 45 },
  { voice: 'Kore', enabled: true, maxSeconds: 120 }
])('saves $voice, $maxSeconds seconds, and only the permitted alert fields', async ({ voice, enabled, maxSeconds }) => {
  const rpc = mockSaveReplies({ rev: 10, modules: [alertRow] })
    .mockResolvedValueOnce({ modules: [alertRow] }).mockResolvedValueOnce({ ok: true });
  const fields = {
    voice, enabled: String(enabled), max_seconds: String(maxSeconds), actor_id: '97',
    broadcaster_user_id: '95', followMessage: 'must stay as stored', is_enabled: 'true', ttsMinBits: '0'
  };

  expect(await save(fields)).toEqual({ ok: true, rev: 10 });
  expect(rpc.mock.calls).toMatchObject([
    usersRead, modulesRead,
    [`${SUB.modules}.patch`, {
      user_id: '42', name: 'alerts', is_enabled: false, expected_rev: 9, configs: { ...alertFields, ttsMinBits: '0' }
    }],
    [`${SUB.projector}.modules.replace`, { user_id: '42', modules: [alertRow] }, 2000],
    [`${SUB.dashboard}.voice_set`, { ...actor, voice, enabled, max_seconds: maxSeconds }, WRITE_TIMEOUT_MS]
  ]);
});

const settingsWrite = (settings: object): RpcCall => [
  `${SUB.dashboard}.voice_set`, { ...actor, ...settings }, WRITE_TIMEOUT_MS
];
const currentSettings = { voice: 'Puck', enabled: true, max_seconds: 45 };

test.each([
  { name: 'voice', fields: { voice: 'Charon' }, rev: 9, merged: { voice: 'Charon' } },
  { name: 'switch', fields: { enabled: 'false', expected_rev: '9' }, rev: 9, merged: { enabled: false } },
  { name: 'limit', fields: { max_seconds: '60', expected_rev: '7' }, rev: 7, merged: { max_seconds: 60 } }
])('saves only the $name and keeps the rest of the allowance', async ({ fields, rev, merged }) => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(allowance);
  if (!('expected_rev' in fields)) rpc.mockResolvedValueOnce({ modules: [alertRow] });
  rpc.mockResolvedValueOnce({ ok: true });

  expect(await savePartial(fields)).toEqual({ ok: true, rev });
  expect(rpc.mock.calls.at(-1)).toEqual(settingsWrite({ ...currentSettings, ...merged }));
  expect(rpc.mock.calls.some(([subject]) => subject === `${SUB.modules}.patch`)).toBe(false);
});

test('saves only the sent alert keys without touching the allowance', async () => {
  const rpc = mockSaveReplies({ rev: 10, modules: [alertRow] }).mockResolvedValueOnce({ modules: [alertRow] });

  expect(await savePartial({ ttsMinBits: '100', cheerTtsStyle: 'calm', expected_rev: '9' })).toEqual({ ok: true, rev: 10 });
  expect(rpc.mock.calls).toMatchObject([
    usersRead, modulesRead,
    [`${SUB.modules}.patch`, { expected_rev: 9, configs: { ttsMinBits: '100', cheerTtsStyle: 'calm' } }],
    [`${SUB.projector}.modules.replace`, { user_id: '42' }, 2000]
  ]);
  expect(rpc.mock.calls.some(([subject]) => subject === `${SUB.dashboard}.voice_set`)).toBe(false);
});

test('writes alerts and settings together when both are sent', async () => {
  const rpc = mockSaveReplies({ rev: 10, modules: [alertRow] })
    .mockResolvedValueOnce({ modules: [alertRow] }).mockResolvedValueOnce({ ok: true });

  expect(await savePartial({ ttsLiveOnly: 'true', voice: 'Zephyr', expected_rev: '9' })).toEqual({ ok: true, rev: 10 });
  expect(rpc.mock.calls.at(-1)).toEqual(settingsWrite({ ...currentSettings, voice: 'Zephyr' }));
});

test.each([
  { name: 'an empty body', fields: {} },
  { name: 'only unknown fields', fields: { actor_id: '97', expected_rev: '9' } },
  { name: 'alert keys without a revision', fields: { ttsLiveOnly: 'true' } },
  { name: 'a malformed revision', fields: { voice: 'Kore', expected_rev: 'x' } },
  { name: 'one bad value among valid ones', fields: { voice: 'Kore', ttsMinBits: '-1', expected_rev: '9' } }
])('rejects $name before any service request', async ({ fields }) => {
  const rpc = spyOn(bus, 'rpc');
  expect(await savePartial(fields)).toMatchObject({ status: 400, data: { error: expect.any(String) } });
  expect(rpc).not.toHaveBeenCalled();
});

test('returns a conflict when Chat Alerts changed after the voice page loaded', async () => {
  const rpc = mockSaveReplies({ rev: 10, conflict: true });
  expect(await save({ expected_rev: '8' })).toMatchObject({ status: 409, data: { conflict: true } });
  expect(rpc.mock.calls).toEqual([
    usersRead, modulesRead, [`${SUB.modules}.patch`, expect.objectContaining({ expected_rev: 8 })]
  ]);
});

test('returns the committed Alerts revision when the Users write fails', async () => {
  mockSaveReplies({ rev: 10, modules: [alertRow] })
    .mockResolvedValueOnce({ modules: [alertRow] }).mockRejectedValueOnce(new Error('Users timeout'));
  expect(await save()).toMatchObject({
    status: 503, data: { error: 'Could not save voice settings. Try again.', revision: 10 }
  });
});

test('blocks saves for a free owner before either write', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(free);
  expect(await save()).toMatchObject({ status: 403 });
  expect(rpc.mock.calls).toEqual([usersRead]);
});

const limitError = 'Enter a whole number from 5 to 120 seconds.';

const invalidSaves: {
  name: string;
  fields?: Record<string, string>;
  session?: typeof owner | null;
  status: number;
  error?: string;
}[] = [
  { name: 'wrong voice casing', fields: { voice: 'kore' }, status: 400 },
  { name: 'missing voice', fields: { voice: '' }, status: 400 },
  { name: 'invalid switch', fields: { enabled: 'yes' }, status: 400 },
  { name: 'below the minimum', fields: { max_seconds: '4' }, status: 400, error: limitError },
  { name: 'above the maximum', fields: { max_seconds: '121' }, status: 400, error: limitError },
  { name: 'fractional seconds', fields: { max_seconds: '5.5' }, status: 400, error: limitError },
  { name: 'empty limit', fields: { max_seconds: '' }, status: 400, error: limitError },
  { name: 'invalid override', fields: { subTtsVoice: 'Unknown' }, status: 400, error: 'Choose a voice from the list.' },
  { name: 'invalid style', fields: { subTtsStyle: 'toString' }, status: 400, error: 'Choose a style from the list.' },
  { name: 'negative bits', fields: { ttsMinBits: '-1' }, status: 400, error: 'Enter a whole number of bits, starting at 0.' },
  { name: 'fractional bits', fields: { ttsMinBits: '1.5' }, status: 400 },
  { name: 'missing minimum bits', fields: { ttsMinBits: '' }, status: 400 },
  { name: 'unsafe minimum bits', fields: { ttsMinBits: '9007199254740992' }, status: 400 },
  { name: 'invalid revision', fields: { expected_rev: '-1' }, status: 400 },
  { name: 'delegate', session: delegate, status: 403 },
  { name: 'signed out', session: null, status: 401 }
];

test.each(invalidSaves)('rejects $name without making service requests', async (row) => {
  const { fields, session = owner, status, error } = row;
  const rpc = spyOn(bus, 'rpc');
  expect(await save(fields, session)).toMatchObject({ status, data: { error: error ?? expect.any(String) } });
  expect(rpc).not.toHaveBeenCalled();
});

test('reports a failed save without claiming it succeeded', async () => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await save()).toMatchObject({
    status: 503, data: { error: 'Could not save voice settings. Try again.' }
  });
});

function rotateOverlay(session: typeof owner | null, setHeaders = mock()) {
  return actions.rotateOverlay({
    request: new Request(`${voiceUrl}?/rotateOverlay`, {
      method: 'POST', body: new URLSearchParams({ actor_id: '97', broadcaster_user_id: '95' })
    }),
    locals: { session, locale: 'en' },
    setHeaders
  } as never);
}

test('returns the new OBS URL to the owner with caching disabled', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ url: overlayUrl });
  const setHeaders = mock();
  expect(await rotateOverlay(owner, setHeaders)).toEqual({ overlayUrl });
  expect(setHeaders.mock.calls).toEqual([[{ 'cache-control': 'no-store' }]]);
  expect(rpc.mock.calls).toEqual([[
    `${SUB.dashboard}.voice_overlay_rotate`, actor, WRITE_TIMEOUT_MS
  ]]);
});

test('view-as staff rotate the OBS link like the owner and the rotation is audited under their id', async () => {
  spyOn(bus, 'rpc').mockResolvedValue({ url: overlayUrl });
  audits.length = 0;
  expect(await rotateOverlay(viewAs)).toEqual({ overlayUrl });
  expect(audits).toEqual([[viewAs, 'voice:overlay_rotate']]);
});

test.each([
  { name: 'delegate', session: delegate, status: 403 },
  { name: 'signed out', session: null, status: 401 }
])('rejects OBS link rotation for $name before calling Users', async ({ session, status }) => {
  const rpc = spyOn(bus, 'rpc');
  expect(await rotateOverlay(session)).toMatchObject({ status, data: { overlayError: expect.any(String) } });
  expect(rpc).not.toHaveBeenCalled();
});

test.each([
  { name: 'plan refused', error: new bus.RpcError('forbidden', 'forbidden'), status: 403 },
  { name: 'Users unavailable', error: new Error('timeout'), status: 503 }
])('reports a failed OBS rotation when $name', async ({ error, status }) => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(error);
  expect(await rotateOverlay(owner)).toMatchObject({ status, data: { overlayError: expect.any(String) } });
});

test.each<Record<string, string>>([
  { maxChars: '0' }, { maxChars: '501' }, { userCooldown: '-1' }, { cooldown: '86401' }, { permission: 'owner' }, { style: 'loud' }, { voice: 'Nobody' }
])('chat command saves reject %o', async (fields) => {
  const rpc = spyOn(bus, 'rpc');
  const result = await actions.saveCommand({
    request: new Request(`${voiceUrl}?/saveCommand`, { method: 'POST', body: new URLSearchParams({ ...fields, expected_rev: '0' }) }),
    locals: { session: owner, locale: 'en' }
  } as never);
  expect(result).toMatchObject({ status: 400 });
  expect(rpc).not.toHaveBeenCalled();
});
