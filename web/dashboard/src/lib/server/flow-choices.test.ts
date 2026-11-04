// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

let rewards: { id: string; title: string }[] = [];
let counters: { name: string }[] = [];
let guilds: { guildId: string; name: string; botPresent: boolean; needsReauth: boolean }[] = [];
let channels: Record<string, { id: string; name: string; type: number; botCanSend?: boolean }[]> = {};
let scenes: { scenes: { id: string; name: string; type: string; instance: string; value: unknown }[]; code?: 'key_rejected' | 'unreachable' } = { scenes: [] };
let failing = false;

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
mock.module('./discord-store', () => ({
  listGuilds: async () => {
    refuse();
    return guilds;
  },
  guildLayout: async ({ guildId }: { guildId: string }) => ({ channels: channels[guildId] ?? [] })
}));
mock.module('./govee-store', () => ({ goveeStore: () => ({ listScenes: async () => scenes }) }));

const { attachChoiceNames, counterChoices, discordChoices, rewardChoices, sceneChoices } = await import('./flow-choices');

beforeEach(() => {
  rewards = [];
  counters = [];
  guilds = [];
  channels = {};
  scenes = { scenes: [] };
  failing = false;
});

describe('flow choices', () => {
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

  test('offers only text channels the bot can post in, with their server', async () => {
    guilds = [{ guildId: 'g1', name: 'Home', botPresent: true, needsReauth: false }];
    channels.g1 = [
      { id: 'c1', name: 'chat', type: 0 },
      { id: 'c2', name: 'voice', type: 2 },
      { id: 'c3', name: 'locked', type: 0, botCanSend: false }
    ];
    expect(await discordChoices('1')).toEqual({ options: [{ value: 'c1', label: '#chat', meta: { guildId: 'g1' } }] });
  });

  test('names the server when more than one is connected', async () => {
    guilds = [
      { guildId: 'g1', name: 'Home', botPresent: true, needsReauth: false },
      { guildId: 'g2', name: 'Club', botPresent: true, needsReauth: false }
    ];
    channels.g1 = [{ id: 'c1', name: 'chat', type: 0 }];
    channels.g2 = [{ id: 'c9', name: 'chat', type: 5 }];
    const { options } = await discordChoices('1');
    expect(options.map((option) => option.label)).toEqual(['#chat (Home)', '#chat (Club)']);
  });

  test('says Discord is not connected when the bot is in no server', async () => {
    guilds = [{ guildId: 'g1', name: 'Home', botPresent: false, needsReauth: false }];
    expect(await discordChoices('1')).toEqual({ options: [], problem: 'notConnected' });
  });

  test('keeps what a scene needs to be played next to its id', async () => {
    scenes = { scenes: [{ id: '7', name: 'Aurora', type: 'devices.capabilities.dynamic_scene', instance: 'lightScene', value: 7 }] };
    expect(await sceneChoices('1', 'AA:BB', 'H6159')).toEqual({
      options: [{ value: '7', label: 'Aurora', meta: { sceneType: 'devices.capabilities.dynamic_scene', sceneInstance: 'lightScene', sceneValue: '7' } }]
    });
  });

  test('tells a rejected Govee key from an unreachable service', async () => {
    scenes = { scenes: [], code: 'key_rejected' };
    expect(await sceneChoices('1', 'AA:BB', 'H6159')).toEqual({ options: [], problem: 'keyRejected' });
    scenes = { scenes: [], code: 'unreachable' };
    expect(await sceneChoices('1', 'AA:BB', 'H6159')).toEqual({ options: [], problem: 'unavailable' });
  });

  test('names a saved reward and channel again from the live lists', async () => {
    rewards = [{ id: 'r1', title: 'Hydrate' }];
    guilds = [{ guildId: 'g1', name: 'Home', botPresent: true, needsReauth: false }];
    channels.g1 = [{ id: 'c1', name: 'chat', type: 0 }];
    const nodes: { type: string; config: Record<string, string> }[] = [
      { type: 'twitch.reward', config: { reward: 'r1' } },
      { type: 'reply.discord', config: { channel: 'c1', guildId: 'g1', message: 'hi' } },
      { type: 'reply.discord', config: { channel: 'gone', guildId: 'g1', message: 'hi' } },
      { type: 'reply.discord', config: { channel: 'c1', channelName: '#kept', message: 'hi' } },
      { type: 'reply.chat', config: { message: 'hi' } }
    ];
    await attachChoiceNames('1', nodes);
    const names: (string | undefined)[] = nodes.map((node) => node.config.rewardTitle ?? node.config.channelName);
    expect(names).toEqual(['Hydrate', '#chat', undefined, '#kept', undefined]);
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
    for (const list of [await rewardChoices('1'), await counterChoices('1'), await discordChoices('1')]) {
      expect(list).toEqual({ options: [], problem: 'unavailable' });
    }
  });
});
