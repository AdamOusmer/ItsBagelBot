// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ChoiceList } from '#lib/flow/choices';
import type { Flow } from '#lib/flow/model';
import type { GoveeTarget } from '#lib/server/govee-store';
import { demoChoices, demoScenes } from '../flow-fixtures';

export * from '#lib/server/flow-choices';

export async function rewardChoices(): Promise<ChoiceList> {
  return demoChoices().rewards;
}

export async function manageableRewardChoices(): Promise<ChoiceList> {
  return demoChoices().rewards;
}

export async function counterChoices(): Promise<ChoiceList> {
  return demoChoices().counters;
}

export async function recipeChoices(_userId: string, current?: Flow['id']): Promise<ChoiceList> {
  return demoChoices(current).recipes;
}

export async function namedCooldownChoices(): Promise<ChoiceList> {
  return demoChoices().namedCooldowns;
}

export async function namedCountdownChoices(): Promise<ChoiceList> {
  return demoChoices().namedCountdowns;
}

export async function sceneChoices(_userId: string, target: GoveeTarget): Promise<ChoiceList> {
  return demoScenes(target.device);
}
