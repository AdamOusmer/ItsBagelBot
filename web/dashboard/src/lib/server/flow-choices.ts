// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { logger } from '@bagel/kit/server/logger';
import { unavailable, type ChoiceList, type ChoiceOption, type ChoiceSource } from '#lib/flow/choices';
import { specFor } from '#lib/flow/model';
import { readRewards } from './channelpoints-store';
import { guildLayout, listGuilds, type DiscordEntry, type DiscordGuildSummary } from './discord-store';
import { goveeStore, type GoveeTarget } from './govee-store';
import { listCounters } from './loyalty-store';

const TEXT_CHANNEL_TYPES = new Set([0, 5]);
const MAX_GUILDS = 3;

type BroadcasterId = string;

type GuildListing = { guild: DiscordGuildSummary; several: boolean };

async function settled(source: ChoiceSource, load: () => Promise<ChoiceList>): Promise<ChoiceList> {
  try {
    return await load();
  } catch (err) {
    logger.warn({ err, source }, '[flows] choice list unavailable');
    return unavailable();
  }
}

export const rewardChoices = (userId: BroadcasterId) =>
  settled('rewards', async () => {
    const { rewards } = await readRewards(userId);
    return { options: rewards.filter((reward) => reward.id).map((reward) => ({ value: reward.id, label: reward.title || reward.id })) };
  });

export const counterChoices = (userId: BroadcasterId) =>
  settled('counters', async () => ({ options: (await listCounters(userId)).map((counter) => ({ value: counter.name, label: counter.name })) }));

const canPost = (channel: DiscordEntry) => TEXT_CHANNEL_TYPES.has(channel.type) && channel.botCanSend !== false;

function channelOptions(channels: DiscordEntry[], { guild, several }: GuildListing): ChoiceOption[] {
  return channels.filter(canPost).map((channel) => ({
    value: channel.id,
    label: `#${channel.name}${several ? ` (${guild.name})` : ''}`,
    meta: { guildId: guild.guildId }
  }));
}

async function guildChannels(userId: BroadcasterId, listing: GuildListing): Promise<ChoiceOption[]> {
  const layout = await guildLayout({ userId, guildId: listing.guild.guildId });
  return channelOptions(layout.channels, listing);
}

export const discordChoices = (userId: BroadcasterId) =>
  settled('discordChannels', async () => {
    const guilds = (await listGuilds({ userId })).filter((guild) => guild.botPresent && !guild.needsReauth).slice(0, MAX_GUILDS);
    if (guilds.length === 0) return unavailable('notConnected');
    const lists = await Promise.all(guilds.map((guild) => guildChannels(userId, { guild, several: guilds.length > 1 })));
    return { options: lists.flat() };
  });

export const sceneChoices = (userId: BroadcasterId, target: GoveeTarget) =>
  settled('scenes', async () => {
    const result = await goveeStore(userId).listScenes(target);
    if (result.code === 'key_rejected') return unavailable('keyRejected');
    if (result.code) return unavailable();
    return {
      options: result.scenes.map((scene) => ({
        value: scene.id,
        label: scene.name || scene.id,
        meta: { sceneType: scene.type, sceneInstance: scene.instance, sceneValue: JSON.stringify(scene.value) }
      }))
    };
  });

type Named = { type: string; config: Record<string, string | number | boolean> };

type Lookup = { node: Named; value: string; nameKey: string };

const NAMED_SOURCES: Partial<Record<ChoiceSource, (userId: BroadcasterId) => Promise<ChoiceList>>> = {
  rewards: rewardChoices,
  discordChannels: discordChoices
};

function lookupsFor(node: Named): [ChoiceSource, Lookup][] {
  return (specFor(node.type)?.fields ?? []).flatMap((field): [ChoiceSource, Lookup][] => {
    if (field.type !== 'choice' || !field.nameKey || !NAMED_SOURCES[field.source]) return [];
    const value = String(node.config[field.key] ?? '');
    return value && !node.config[field.nameKey] ? [[field.source, { node, value, nameKey: field.nameKey }]] : [];
  });
}

function unnamed(nodes: readonly Named[]): Map<ChoiceSource, Lookup[]> {
  const wanted = new Map<ChoiceSource, Lookup[]>();
  for (const [source, lookup] of nodes.flatMap(lookupsFor)) wanted.set(source, [...(wanted.get(source) ?? []), lookup]);
  return wanted;
}

export async function attachChoiceNames(userId: BroadcasterId, nodes: readonly Named[]): Promise<void> {
  await Promise.all(
    [...unnamed(nodes)].map(async ([source, lookups]) => {
      const { options } = await NAMED_SOURCES[source]!(userId);
      for (const { node, value, nameKey } of lookups) {
        const label = options.find((option) => option.value === value)?.label;
        if (label) node.config[nameKey] = label;
      }
    })
  );
}
