// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const CHOICE_SOURCES = ['rewards', 'counters', 'scenes'] as const;

export type ChoiceSource = (typeof CHOICE_SOURCES)[number];

export type ChoiceOption = { value: string; label: string; meta?: Record<string, string> };

export type ChoiceProblem = 'unavailable' | 'keyRejected';

export type ChoiceList = { options: ChoiceOption[]; problem?: ChoiceProblem };

export const isChoiceSource = (value: unknown): value is ChoiceSource => CHOICE_SOURCES.some((source) => source === value);

export const unavailable = (problem: ChoiceProblem = 'unavailable'): ChoiceList => ({ options: [], problem });

export function withSaved(list: ChoiceList, value: string, name: string): ChoiceOption[] {
  if (!value || list.options.some((option) => option.value === value)) return list.options;
  return [...list.options, { value, label: name || value }];
}
