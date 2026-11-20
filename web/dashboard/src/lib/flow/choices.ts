// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { RECIPE_NAME_PATTERN } from '@bagel/kit/automations/limits';
import type { FlowNode } from './model';

export const CHOICE_SOURCES = ['rewards', 'manageableRewards', 'counters', 'scenes', 'namedCooldowns', 'namedCountdowns', 'recipes'] as const;

export type ChoiceSource = (typeof CHOICE_SOURCES)[number];

export type ChoiceOption = { value: string; label: string; meta?: Record<string, string> };

export type ChoiceProblem = 'unavailable' | 'keyRejected';

export type ChoiceList = { options: ChoiceOption[]; problem?: ChoiceProblem };

export const isChoiceSource = (value: unknown): value is ChoiceSource => CHOICE_SOURCES.some((source) => source === value);

export const unavailable = (problem: ChoiceProblem = 'unavailable'): ChoiceList => ({ options: [], problem });

type RecipeChoice = { id: FlowNode['id']; name: string };

export function recipeOptions(recipes: readonly RecipeChoice[], current?: RecipeChoice['id']): ChoiceOption[] {
  return recipes.filter(({ id }) => id !== current).map(({ id, name }) => ({ value: id, label: name }));
}

export function withSaved(list: ChoiceList, value: string, name: string): ChoiceOption[] {
  if (!value || list.options.some((option) => option.value === value)) return list.options;
  return [...list.options, { value, label: name || value }];
}

export function withDraftNames(list: ChoiceList, source: ChoiceSource, nodes: readonly FlowNode[]): ChoiceList {
  const type = { namedCooldowns: 'cond.cooldown', namedCountdowns: 'reply.countdown_start' }[source as 'namedCooldowns' | 'namedCountdowns'];
  const names = nodes.filter((node) => node.type === type && !node.config.disabled && !node.config.perUser)
    .map((node) => String(node.config.name ?? '').trim().toLowerCase()).filter((name) => RECIPE_NAME_PATTERN.test(name));
  const options = new Map(list.options.map((option) => [option.value, option]));
  for (const name of names) options.set(name, { value: name, label: name });
  return { ...list, options: [...options.values()] };
}
