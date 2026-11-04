// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { FLOW_FILE_FORMAT, type Flow } from '@bagel/kit/automations';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

const calls: Array<{ subject: string; body: any }> = [];
let replies: Record<string, unknown> = {};
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: any) => {
    calls.push({ subject, body });
    if (!(subject in replies)) throw new Error(`Unexpected RPC: ${subject}`);
    const reply = replies[subject];
    return typeof reply === 'function' ? (reply as (b: any) => unknown)(body) : reply;
  }
}));
mock.module('./edge-purge', () => ({ schedulePurgeChannel: () => {}, scheduleLeaderboardPurge: () => {} }));
mock.module('@bagel/kit/server/rate-limit', () => ({
  ValkeyRateLimiter: class {
    async check() {
      return { allowed: true, retryAfterSec: 0 };
    }
  }
}));

const { fabric, SUB } = await import('./services');
const { flowShareActions } = await import('./flow-share');

const AUTO = 'bagel.rpc.modules.automation';
const AUDIT = `${SUB.audit}.append`;
const actions = flowShareActions({ flowPreview: (flow: Flow) => ({ steps: flow.nodes.length }) });

const SECRET = 'SECRET-6b1d0f42';
const OTHER = 'OTHER-ACCOUNT-31c9';
const chat = (text: string) => ({ type: 'chat', params: { text } });

const lights = {
  type: 'lights',
  params: { device: `AB:CD:EF:01:23:45:67:89`, sku: 'H6159', device_name: 'Desk strip', mode: 'scene', scene_id: 'lightScene:1:2', scene_name: 'Sunrise', scene_type: 'devices.capabilities.dynamic_scene', scene_instance: 'lightScene', scene_value: '{"paramId":1,"id":2}' }
};
const mine = {
  id: 'auto-1',
  user_id: 7,
  name: 'Mine',
  enabled: true,
  revision: 3,
  definition: {
    version: 1,
    trigger: { type: 'reward', params: { reward_id: `reward-${SECRET}` } },
    conditions: [{ type: 'users', params: { allow: ['realviewer'], allow_ids: ['4242424242'] } }],
    actions: [lights, { type: 'http_post', params: { url: `https://hooks.example.org/${SECRET}`, body: SECRET }, enabled: true }, chat('hi {user}')]
  }
};
const theirs = {
  id: 'auto-other',
  user_id: 9,
  name: OTHER,
  enabled: true,
  revision: 1,
  definition: { version: 1, trigger: { type: 'manual', params: {} }, actions: [chat(OTHER)] }
};

const commandRows = (userId: string) =>
  userId === '7'
    ? [{ name: 'hug', response: 'hugs {user} {urlfetch:weather}', is_active: true, perm: 'everyone', allowed_user_id: `viewer-${SECRET}`, bump_counter: `ctr-${SECRET}`, fetch_key: SECRET, uses: '12' }]
    : [{ name: 'other', response: OTHER, is_active: true, allowed_user_id: OTHER }];

const moduleRows = (userId: string) =>
  userId === '7'
    ? [
        { name: 'alerts', is_enabled: true, configs: { followMessage: 'welcome', followEnabled: 'on', webhookUrl: SECRET, apiToken: SECRET }, revision: 4 },
        { name: 'govee', is_enabled: true, configs: { bindings: [{ device: { device: `dev-${SECRET}`, sku: 'H6159' }, reward: { id: `rw-${SECRET}` }, apiKey: SECRET }] }, revision: 2 },
        { name: 'spotify', is_enabled: true, configs: { accessToken: SECRET, refreshToken: SECRET }, revision: 1 }
      ]
    : [{ name: 'alerts', is_enabled: true, configs: { followMessage: OTHER }, revision: 1 }];

function stubChannels() {
  const byUser = (rows: (id: string) => unknown[], key: string) => (body: any) => ({ [key]: rows(String(body.user_id)) });
  replies['bagel.rpc.commands.list'] = byUser(commandRows, 'commands');
  replies['bagel.rpc.projector.dashboard.commands.get'] = byUser(commandRows, 'commands');
  replies['bagel.rpc.modules.list'] = byUser(moduleRows, 'modules');
  replies['bagel.rpc.projector.dashboard.modules.get'] = byUser(moduleRows, 'modules');
  replies[`${AUTO}.list`] = (body: any) => ({ automations: String(body.user_id) === '7' ? [mine] : [theirs] });
  replies[AUDIT] = {};
  replies[`${SUB.dashboard}.state_get`] = { status: 'paid' };
}

const withKeys = (...keys: string[]) => {
  const form = new FormData();
  for (const key of keys) form.append('flow', key);
  return form;
};

function post(name: keyof typeof actions, form: FormData, session: unknown) {
  const request = new Request('https://dashboard.itsbagelbot.com/flows', { method: 'POST', body: form });
  const locals = { session, locale: 'en', accountState: { value: { status: 'paid', active: true, onboarded: true } } };
  return actions[name]({ request, locals } as never) as Promise<any>;
}

const ALL_OWN = ['automation:auto-1', 'command:hug', 'alert:follow'];
const everyUserId = () => calls.flatMap((call) => [call.body?.user_id, call.body?.broadcaster_id]).filter((id) => id !== undefined).map(String);

beforeEach(() => {
  calls.length = 0;
  replies = {};
  fabric.cache.invalidate('automations:7', 'commands:7', 'modules:7', 'automations:9', 'commands:9', 'modules:9');
  stubChannels();
});

describe('what reaches an exported file', () => {
  test('only definitions: no key, token, id or setting from the rows around them', async () => {
    const result = await post('flowExport', withKeys(...ALL_OWN), { user_id: '7' });
    expect(result.ok).toBe(true);
    const parsed = JSON.parse(result.file);
    expect(parsed.flows.map((flow: { name: string }) => flow.name)).toEqual(['Mine', '!hug', 'Follow alert']);
    for (const leak of [SECRET, OTHER, 'realviewer', '4242424242', 'hooks.example.org', 'AB:CD:EF', 'Desk strip', 'govee', 'spotify', 'reward-', 'auto-1', 'revision', 'user_id']) {
      expect(result.file, leak).not.toContain(leak);
    }
    expect(Object.keys(parsed).sort()).toEqual(['flows', 'format', 'version']);
    for (const flow of parsed.flows) expect(Object.keys(flow).sort()).toEqual(['definition', 'name', 'ref']);
  });

  test('a command export keeps its words and fetch name, not its owner, counter or fetch key', async () => {
    const result = await post('flowExport', withKeys('command:hug'), { user_id: '7' });
    const definition = JSON.parse(result.file).flows[0].definition;
    expect(JSON.stringify(definition)).toContain('{urlfetch:weather}');
    expect(result.file).not.toContain('viewer-');
    expect(result.file).not.toContain('ctr-');
  });

  test('nothing is read outside the three stores an export needs', async () => {
    await post('flowExport', withKeys(...ALL_OWN), { user_id: '7' });
    const allowed = [`${AUTO}.list`, 'bagel.rpc.commands.list', 'bagel.rpc.projector.dashboard.commands.get', 'bagel.rpc.modules.list', 'bagel.rpc.projector.dashboard.modules.get'];
    expect(calls.map((call) => call.subject).filter((subject) => !allowed.includes(subject))).toEqual([]);
  });
});

describe('whose rows an export reads', () => {
  test('forged keys, ids and extra form fields from another account get nothing', async () => {
    const form = withKeys('automation:auto-other', 'command:other', 'alert:follow');
    for (const field of ['user_id', 'broadcaster_id', 'delegate_of', 'uid', 'ref', 'id']) form.append(field, '9');
    const result = await post('flowExport', form, { user_id: '7' });
    expect(result.file).not.toContain(OTHER);
    expect(result.missing).toBe(2);
    expect(new Set(everyUserId())).toEqual(new Set(['7']));
  });

  test('a delegate reads the broadcaster and never the delegate account', async () => {
    const delegate = { user_id: '9', delegate_of: '7', sections: ['modules'] };
    fabric.cache.invalidate('automations:7', 'commands:7', 'modules:7');
    const result = await post('flowExport', withKeys(...ALL_OWN, 'automation:auto-other'), delegate);
    expect(result.exported).toBe(3);
    expect(result.file).not.toContain(OTHER);
    expect(new Set(everyUserId())).toEqual(new Set(['7']));
  });

  test('an impersonated session exports the impersonated account and audits only a count', async () => {
    const session = { user_id: '7', impersonator_id: '1', impersonator_login: 'admin' };
    await post('flowExport', withKeys(...ALL_OWN), session);
    const audits = calls.filter((call) => call.subject === AUDIT);
    expect(audits).toHaveLength(1);
    expect(audits[0]!.body).toMatchObject({ action: 'dashboard:automations:flow_export', target: '7', detail: '3' });
    expect(new Set(everyUserId().filter((id) => id !== '1'))).toEqual(new Set(['7']));
  });
});

describe('what an import writes and what it says back', () => {
  const hostile = {
    format: FLOW_FILE_FORMAT,
    version: 1,
    flows: [
      {
        ref: 'f1',
        name: 'Planted',
        definition: {
          version: 1,
          trigger: { type: 'reward', params: { reward_id: `reward-${SECRET}` } },
          conditions: [{ type: 'users', params: { allow: ['realviewer'], allow_ids: ['4242424242'] } }],
          actions: [lights, { type: 'http_post', params: { url: `https://hooks.example.org/${SECRET}`, body: SECRET } }, chat('hi'), { type: 'run', params: { automation_id: 'auto-other' } }]
        }
      }
    ]
  };

  function commit(text: string, session: unknown) {
    const form = new FormData();
    form.set('file', new File([text], 'recipes.json', { type: 'application/json' }));
    form.append('index', '0');
    return post('flowImportCommit', form, session);
  }

  let next = 0;
  beforeEach(() => {
    next = 0;
    replies[`${AUTO}.upsert`] = (body: any) => ({ automation: { id: body.id ?? `new-${++next}`, user_id: Number(body.user_id), name: body.name, enabled: body.enabled, revision: 1, definition: body.definition } });
  });

  test('rows go to the effective account, off, with nothing from the file that names a resource', async () => {
    const result = await commit(JSON.stringify(hostile), { user_id: '9', delegate_of: '7', sections: ['modules'] });
    expect(result.results[0].status).toBe('created');
    const writes = calls.filter((call) => call.subject === `${AUTO}.upsert`).map((call) => call.body);
    expect(writes.length).toBeGreaterThan(0);
    for (const write of writes) {
      expect(write.user_id).toBe('7');
      expect(write.enabled).toBe(false);
      const text = JSON.stringify(write);
      for (const leak of [SECRET, 'realviewer', '4242424242', 'hooks.example.org', 'AB:CD:EF', 'Desk strip', 'auto-other']) expect(text, leak).not.toContain(leak);
      const steps = write.definition.actions.filter((step: { type: string }) => step.type === 'lights' || step.type === 'http_post');
      expect(steps.every((step: { enabled: boolean }) => step.enabled === false)).toBe(true);
    }
  });

  test('replies and the audit line carry codes and counts, never file text', async () => {
    const session = { user_id: '7', impersonator_id: '1', impersonator_login: 'admin' };
    const previewForm = new FormData();
    previewForm.set('file', new File([JSON.stringify(hostile)], 'recipes.json'));
    const preview = await post('flowImportPreview', previewForm, session);
    expect(JSON.stringify(preview)).not.toContain(SECRET);
    const committed = await commit(JSON.stringify(hostile), session);
    expect(JSON.stringify(committed)).not.toContain(SECRET);
    const audit = calls.filter((call) => call.subject === AUDIT).at(-1)!.body;
    expect(audit.detail).toBe('1');
    expect(JSON.stringify(audit)).not.toContain(SECRET);
    const broken = await commit(`{"format":"${SECRET}`, session);
    expect(JSON.stringify(broken)).not.toContain(SECRET);
  });

  test('a failing store never echoes its error text to the browser', async () => {
    replies[`${AUTO}.upsert`] = () => {
      throw new Error(`down ${SECRET}`);
    };
    const result = await commit(JSON.stringify(hostile), { user_id: '7' });
    expect(JSON.stringify(result)).not.toContain(SECRET);
    replies[`${AUTO}.list`] = () => {
      throw new Error(`list ${SECRET}`);
    };
    fabric.cache.invalidate('automations:7');
    const failed = await post('flowExport', withKeys('automation:auto-1'), { user_id: '7' });
    expect(JSON.stringify(failed)).not.toContain(SECRET);
  });
});
