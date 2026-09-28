// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/** Data shared by the interactive select adapters. */
export interface SelectOption {
  value: string;
  label: string;
  description?: string;
  group?: string;
  disabled?: boolean;
  /** Extra searchable text, such as aliases or a full identifier. */
  searchText?: string;
  /** Override the label shown in the closed control. */
  triggerLabel?: string;
}

export function normalizeSelectQuery(text: string): string {
  return text.trim().toLowerCase().normalize('NFD').replace(/\p{Diacritic}/gu, '').replace(/[_\s]+/g, ' ');
}

export function filterSelectOptions(options: readonly SelectOption[], query: string): SelectOption[] {
  const words = normalizeSelectQuery(query).split(' ').filter(Boolean);
  return options.filter((option) => {
    const text = normalizeSelectQuery([option.label, option.value, option.description, option.group, option.searchText].filter(Boolean).join(' '));
    return words.every((word) => text.includes(word));
  });
}

export function optionIndexAt(list: Element, target: EventTarget | null): number {
  const option = target instanceof Element ? target.closest<HTMLElement>('[role="option"]') : null;
  return option && list.contains(option) ? Number(option.dataset.index) : -1;
}

/** Wrap keyboard navigation, skipping disabled options, including an all-disabled list. */
export function nextEnabledOption(options: readonly SelectOption[], active: number, direction: 1 | -1 | 'first' | 'last'): number {
  const step = direction === 'last' || direction === -1 ? -1 : 1;
  let index = direction === 'first' ? -1 : direction === 'last' ? options.length : active;
  for (let count = 0; count < options.length; count++) {
    index = (index + step + options.length) % options.length;
    if (!options[index].disabled) return index;
  }
  return -1;
}
