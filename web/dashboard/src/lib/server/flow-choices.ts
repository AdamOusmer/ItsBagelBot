// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { ownedCooldownNames, ownedCountdownNames } from '@bagel/kit/automations';
import { logger } from '@bagel/kit/server/logger';
import { unavailable, type ChoiceList, type ChoiceSource } from '#lib/flow/choices';
import { specFor } from '#lib/flow/model';
import { readRewards } from './channelpoints-store';
import { listAutomations } from './automations-store';
import { goveeStore, type GoveeTarget } from './govee-store';
import { listCounters } from './loyalty-store';
import { callReward } from './reward-wire';

type BroadcasterId = string;

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

export const manageableRewardChoices = (userId: BroadcasterId) =>
  settled('manageableRewards', async () => {
    const reply = await callReward('list', { broadcaster_id: userId });
    if (reply.missing_scope || reply.error) return unavailable();
    return { options: (reply.rewards ?? []).flatMap((reward) => (reward.id ? [{ value: reward.id, label: reward.title || reward.id }] : [])) };
  });

export const counterChoices = (userId: BroadcasterId) =>
  settled('counters', async () => ({ options: (await listCounters(userId)).map((counter) => ({ value: counter.name, label: counter.name })) }));

// Every recipe's own cooldown condition can own a name; cooldown_add picks among them by name alone, so a channel
// naming the same timer twice just offers one option for it, labelled with whichever recipe comes first.
export const namedCooldownChoices = (userId: BroadcasterId) =>
  settled('namedCooldowns', async () => {
    const automations = await listAutomations(userId);
    const seen = new Set<string>();
    const options: ChoiceList['options'] = [];
    for (const automation of automations) {
      for (const name of ownedCooldownNames(automation.definition)) {
        if (seen.has(name)) continue;
        seen.add(name);
        options.push({ value: name, label: `${name}, in ${automation.name}` });
      }
    }
    return { options };
  });

export const namedCountdownChoices = (userId: BroadcasterId) =>
  settled('namedCountdowns', async () => {
    const automations = await listAutomations(userId);
    const seen = new Set<string>();
    const options: ChoiceList['options'] = [];
    for (const automation of automations) {
      for (const name of ownedCountdownNames(automation.definition)) {
        if (seen.has(name)) continue;
        seen.add(name);
        options.push({ value: name, label: `${name}, in ${automation.name}` });
      }
    }
    return { options };
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
  manageableRewards: manageableRewardChoices
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
