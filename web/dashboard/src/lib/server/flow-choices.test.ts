// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

let rewards: { id: string; title: string }[] = [];
let counters: { name: string }[] = [];
let scenes: { scenes: { id: string; name: string; type: string; instance: string; value: unknown }[]; code?: 'key_rejected' | 'unreachable' } = { scenes: [] };
let failing = false;
let rewardReply: { rewards?: { id?: string; title: string }[]; missing_scope?: boolean; error?: string } = {};
let rewardCalls: string[] = [];

const refuse = () => {
  if (failing) throw new Error('rpc down');
};

mock.module('./channelpoints-store', () => ({
  readRewards: async () => {
    refuse();
    return { enabled: true, rewards };
  }
}));
mock.module('./loyalty-store', () => ({
  listCounters: async () => {
    refuse();
    return counters;
  }
}));
mock.module('./govee-store', () => ({ goveeStore: () => ({ listScenes: async () => scenes }) }));
mock.module('./reward-wire', () => ({
  callReward: async (verb: string, req: { broadcaster_id: string }) => {
    refuse();
    rewardCalls.push(`${verb}:${req.broadcaster_id}`);
    return rewardReply;
  }
}));

const { attachChoiceNames, counterChoices, manageableRewardChoices, rewardChoices, sceneChoices } = await import('./flow-choices');

beforeEach(() => {
  rewardReply = {};
  rewardCalls = [];
  rewards = [];
  counters = [];
  scenes = { scenes: [] };
  failing = false;
});

describe('flow choices', () => {
  test('the reward change picker lists what Twitch says the bot can manage', async () => {
    rewardReply = { rewards: [{ id: 'r1', title: 'Hydrate' }, { title: 'No id' }] };
    expect(await manageableRewardChoices('1')).toEqual({ options: [{ value: 'r1', label: 'Hydrate' }] });
    expect(rewardCalls).toEqual(['list:1']);
  });

  test('the reward change picker reports a refusal or an outage instead of an empty list', async () => {
    rewardReply = { missing_scope: true };
    expect(await manageableRewardChoices('1')).toEqual({ options: [], problem: 'unavailable' });
    rewardReply = { error: 'nope' };
    expect(await manageableRewardChoices('1')).toEqual({ options: [], problem: 'unavailable' });
    failing = true;
    expect(await manageableRewardChoices('1')).toEqual({ options: [], problem: 'unavailable' });
  });

  test('offers rewards that exist on Twitch, by id', async () => {
    rewards = [
      { id: 'r1', title: 'Hydrate' },
      { id: '', title: 'Not synced yet' }
    ];
    expect(await rewardChoices('1')).toEqual({ options: [{ value: 'r1', label: 'Hydrate' }] });
  });

  test('offers counters by name', async () => {
    counters = [{ name: 'deaths' }];
    expect(await counterChoices('1')).toEqual({ options: [{ value: 'deaths', label: 'deaths' }] });
  });

  test('keeps what a scene needs to be played next to its id', async () => {
    scenes = { scenes: [{ id: '7', name: 'Aurora', type: 'devices.capabilities.dynamic_scene', instance: 'lightScene', value: 7 }] };
    expect(await sceneChoices('1', { device: 'AA:BB', sku: 'H6159' })).toEqual({
      options: [{ value: '7', label: 'Aurora', meta: { sceneType: 'devices.capabilities.dynamic_scene', sceneInstance: 'lightScene', sceneValue: '7' } }]
    });
  });

  test('tells a rejected Govee key from an unreachable service', async () => {
    scenes = { scenes: [], code: 'key_rejected' };
    expect(await sceneChoices('1', { device: 'AA:BB', sku: 'H6159' })).toEqual({ options: [], problem: 'keyRejected' });
    scenes = { scenes: [], code: 'unreachable' };
    expect(await sceneChoices('1', { device: 'AA:BB', sku: 'H6159' })).toEqual({ options: [], problem: 'unavailable' });
  });

  test('names a saved reward again from the live list', async () => {
    rewards = [{ id: 'r1', title: 'Hydrate' }];
    const nodes: { type: string; config: Record<string, string> }[] = [
      { type: 'twitch.reward', config: { reward: 'r1' } },
      { type: 'twitch.reward', config: { reward: 'gone' } },
      { type: 'twitch.reward', config: { reward: 'r1', rewardTitle: 'Kept' } },
      { type: 'reply.chat', config: { message: 'hi' } }
    ];
    await attachChoiceNames('1', nodes);
    const names: (string | undefined)[] = nodes.map((node) => node.config.rewardTitle);
    expect(names).toEqual(['Hydrate', undefined, 'Kept', undefined]);
  });

  test('a flow with nothing to name asks for no lists', async () => {
    failing = true;
    const nodes = [{ type: 'reply.chat', config: { message: 'hi' } }];
    await attachChoiceNames('1', nodes);
    expect(nodes[0]!.config).toEqual({ message: 'hi' });
  });

  test('a failed read leaves the id to show', async () => {
    failing = true;
    const nodes = [{ type: 'twitch.reward', config: { reward: 'r1' } }];
    await attachChoiceNames('1', nodes);
    expect(nodes[0]!.config).toEqual({ reward: 'r1' });
  });

  test('a failed read becomes an unavailable list instead of an error', async () => {
    failing = true;
    for (const list of [await rewardChoices('1'), await counterChoices('1')]) {
      expect(list).toEqual({ options: [], problem: 'unavailable' });
    }
  });
});
