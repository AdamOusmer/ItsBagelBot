// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import type { RewardReply } from '../flow/replies';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

const calls: Array<{ subject: string; body: any }> = [];
let replies: Record<string, unknown> = {};
const nats = await import('@bagel/kit/server/nats');
const answer = async (subject: string, body: any) => {
  calls.push({ subject, body });
  if (!(subject in replies)) throw new Error(`Unexpected RPC: ${subject}`);
  return replies[subject];
};
mock.module('@bagel/kit/server/nats', () => ({ ...nats, publish: async () => {}, rpc: answer, rpcReply: answer }));

const { liveRewardSave } = await import('./flow-page');
const { parseRewardDraft } = await import('./flow-reward');

const REWARD = 'rpc.outgress.channelpoints';
const locals = { session: { user_id: '7' }, locale: 'en' } as unknown as App.Locals;
const draft = { rewardId: '', title: 'Hydrate', cost: 500, color: '#00ccff', cooldown: 60, inputRequired: false };
const twitchReward = (id: string) => ({ reward: { id, title: 'Hydrate', cost: 500, background_color: '#00ccff', global_cooldown_enabled: true, global_cooldown_seconds: 60 } });
const verbsCalled = () => calls.map((call) => call.subject.slice(REWARD.length + 1));

beforeEach(() => {
  calls.length = 0;
  replies = {};
});

describe('creating a recipe reward without saving the recipe', () => {
  type Saving = { name: string; rewardId: string; wire: Record<string, unknown>; verbs: string[]; stored: string };

  test.each<Saving>([
    { name: 'creates a new reward', rewardId: '', wire: { list: { rewards: [] }, create: twitchReward('rw-1') }, verbs: ['list', 'create'], stored: 'rw-1' },
    { name: 'adopts a same-title reward instead of duplicating it', rewardId: '', wire: { list: { rewards: [{ id: 'rw-7', title: 'hydrate ' }] }, update: twitchReward('rw-7') }, verbs: ['list', 'update'], stored: 'rw-7' },
    { name: 'updates a reward it already owns in place', rewardId: 'rw-3', wire: { update: twitchReward('rw-3') }, verbs: ['update'], stored: 'rw-3' },
    { name: 'treats a setup placeholder as a new reward', rewardId: 'setup-required', wire: { list: { rewards: [] }, create: twitchReward('rw-4') }, verbs: ['list', 'create'], stored: 'rw-4' }
  ])('$name', async ({ rewardId, wire, verbs, stored }) => {
    for (const [verb, reply] of Object.entries(wire)) replies[`${REWARD}.${verb}`] = reply;
    const reply = await liveRewardSave(locals, { ...draft, rewardId });
    expect(reply).toEqual({ ok: true, rewardId: stored, title: 'Hydrate', cost: 500, color: '#00ccff', cooldown: 60 });
    expect(verbsCalled()).toEqual(verbs);
  });

  test.each([
    ['a missing Twitch permission is flagged and localized', { list: { rewards: [] }, create: { missing_scope: true } }, { missingScope: true, error: 'Reconnect your Twitch account to manage channel points. One extra permission is needed.' }],
    ['a refusal from Twitch is reported as a readable failure', { list: { rewards: [] }, create: { error: 'title taken' } }, { missingScope: false, error: 'Twitch did not accept the reward. Check the title and cost, then try again.' }]
  ] as [string, Record<string, unknown>, Record<string, unknown>][])('%s', async (_name, wire, expected) => {
    for (const [verb, reply] of Object.entries(wire)) replies[`${REWARD}.${verb}`] = reply;
    expect(await liveRewardSave(locals, draft)).toEqual({ ok: false, ...expected } as RewardReply);
  });

  test('an unreachable outgress service becomes a readable failure', async () => {
    const reply = await liveRewardSave(locals, draft);
    expect(reply).toMatchObject({ ok: false, missingScope: false, error: 'Twitch did not accept the reward. Check the title and cost, then try again.' });
  });
});

describe('reading a reward request', () => {
  test.each([
    ['a full request', { title: ' Hydrate ', cost: 500, color: '#00ccff', cooldown: 60, rewardId: 'rw-1', inputRequired: true }, { ...draft, rewardId: 'rw-1', inputRequired: true }],
    ['a request without optional fields', { title: 'Hydrate', cost: 5 }, { rewardId: '', title: 'Hydrate', cost: 5, color: '', cooldown: 0, inputRequired: false }]
  ])('accepts %s', (_name, body, expected) => {
    expect(parseRewardDraft(JSON.stringify(body))).toEqual(expected);
  });

  test.each([
    ['not json', 'nope'],
    ['an array', '[]'],
    ['an empty title', JSON.stringify({ title: '  ', cost: 5 })],
    ['a title over 45 characters', JSON.stringify({ title: 'x'.repeat(46), cost: 5 })],
    ['a zero cost', JSON.stringify({ title: 'a', cost: 0 })],
    ['a fractional cooldown', JSON.stringify({ title: 'a', cost: 5, cooldown: 1.5 })],
    ['a cooldown over seven days', JSON.stringify({ title: 'a', cost: 5, cooldown: 604_801 })],
    ['a colour that is not hex', JSON.stringify({ title: 'a', cost: 5, color: 'red' })]
  ])('rejects %s', (_name, text) => {
    expect(parseRewardDraft(text)).toBeNull();
  });
});
