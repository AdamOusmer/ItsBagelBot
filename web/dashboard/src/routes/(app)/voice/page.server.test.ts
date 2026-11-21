// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { ALERT_VOICE_CONFIG_DEFAULTS, alertVoiceKeys, VOICE_ALERT_KINDS } from '@bagel/kit/voice-plan';
import type { VoiceAllowance } from '#lib/server/services/voice';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
mock.module('#lib/server/edge-purge', () => ({
  schedulePurgeChannel: () => {},
  scheduleLeaderboardPurge: () => {}
}));

const bus = await import('@bagel/kit/server/nats');
const { translate } = await import('@bagel/kit/i18n');
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
  name: 'alerts', is_enabled: false, revision: 9, configs: { cheerTtsStyle: 'calm', followMessage: 'Chat only' }
};
const alertFields: Record<string, string> = {
  ...ALERT_VOICE_CONFIG_DEFAULTS, followTts: 'true', cheerTtsStyle: 'calm', raidTtsVoice: 'Charon'
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
  { name: 'view-as', session: viewAs, reply: null, readOnly: true }
])('loads voice data for $name', async ({ session, reply, readOnly }) => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(reply).mockResolvedValueOnce({ modules: [alertRow] });
  const alerts = reply?.reason === 'ok'
    ? { config: { ...ALERT_VOICE_CONFIG_DEFAULTS, cheerTtsStyle: 'calm' }, revision: 9 }
    : null;

  expect(await load({ locals: { session, locale: 'en' } } as never)).toEqual({
    readOnly, allowance: reply, locked: reply?.reason === 'no_plan', alerts
  });
  expect(rpc.mock.calls).toEqual([...(readOnly ? [] : [usersRead]), ...(alerts ? [modulesRead] : [])]);
});

test('an unavailable allowance degrades without inventing a free plan', async () => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await load({ locals: { session: owner, locale: 'en' } } as never)).toEqual({
    readOnly: false, allowance: null, locked: false, alerts: null
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

function mockSaveReplies(moduleReply: { conflict?: boolean; rev: number; modules?: unknown[] }) {
  return spyOn(bus, 'rpc').mockResolvedValueOnce(allowance).mockResolvedValueOnce({ modules: [alertRow] })
    .mockResolvedValueOnce(moduleReply);
}

test.each(['en', 'fr', 'es', 'de', 'pt-br', 'ru'])('loads and saves spoken templates for %s', async (locale) => {
  const row = {
    ...alertRow, configs: { ...alertRow.configs, followTtsMessage: 'My custom follow alert', raidTtsMessage: '' }
  };
  const rpc = spyOn(bus, 'rpc')
    .mockResolvedValueOnce(allowance).mockResolvedValueOnce({ modules: [row] })
    .mockResolvedValueOnce(allowance).mockResolvedValueOnce({ modules: [row] })
    .mockResolvedValueOnce({ rev: 10, modules: [row] })
    .mockResolvedValueOnce({ modules: [row] }).mockResolvedValueOnce({ ok: true });
  const { alerts } = (await load({ locals: { session: owner, locale } } as never)) as {
    alerts?: { config: Record<string, string> };
  };
  const messages = Object.fromEntries(VOICE_ALERT_KINDS.map((kind) => [
    alertVoiceKeys(kind).message, translate(locale, `voice.template.${kind}`)
  ]));
  expect(alerts?.config).toMatchObject({ ...messages, followTtsMessage: row.configs.followTtsMessage, raidTtsMessage: '' });
  expect(await save(alerts?.config)).toEqual({ ok: true });
  expect(rpc).toHaveBeenCalledWith(
    `${SUB.modules}.patch`, expect.objectContaining({ configs: alerts?.config })
  );
});

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

  expect(await save(fields)).toEqual({ ok: true });
  expect(rpc.mock.calls).toMatchObject([
    usersRead, modulesRead,
    [`${SUB.modules}.patch`, {
      user_id: '42', name: 'alerts', is_enabled: false, expected_rev: 9, configs: { ...alertFields, ttsMinBits: '0' }
    }],
    [`${SUB.projector}.modules.replace`, { user_id: '42', modules: [alertRow] }, 2000],
    [`${SUB.dashboard}.voice_set`, { ...actor, voice, enabled, max_seconds: maxSeconds }, WRITE_TIMEOUT_MS]
  ]);
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
  { name: 'invalid override', fields: { raidTtsVoice: 'Unknown' }, status: 400, error: 'Choose a voice from the list.' },
  { name: 'invalid style', fields: { raidTtsStyle: 'toString' }, status: 400, error: 'Choose a style from the list.' },
  { name: 'negative bits', fields: { ttsMinBits: '-1' }, status: 400, error: 'Enter a whole number of bits, starting at 0.' },
  { name: 'fractional bits', fields: { ttsMinBits: '1.5' }, status: 400 },
  { name: 'missing minimum bits', fields: { ttsMinBits: '' }, status: 400 },
  { name: 'unsafe minimum bits', fields: { ttsMinBits: '9007199254740992' }, status: 400 },
  { name: 'invalid revision', fields: { expected_rev: '-1' }, status: 400 },
  { name: 'newline in a spoken template', fields: { followTtsMessage: 'Hello\nthere' }, status: 400,
    error: 'Response cannot contain control characters.' },
  { name: 'tab in a spoken template', fields: { cheerTtsMessage: 'Thanks\t' }, status: 400 },
  { name: 'C1 control in a spoken template', fields: { raidTtsMessage: '\u0085Welcome' }, status: 400 },
  { name: 'delegate', session: delegate, status: 403 },
  { name: 'view-as', session: viewAs, status: 403 },
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

test.each([
  { name: 'delegate', session: delegate, status: 403 },
  { name: 'view-as', session: viewAs, status: 403 },
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
