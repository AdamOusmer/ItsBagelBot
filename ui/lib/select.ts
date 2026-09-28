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

export type SelectDirection = 1 | -1 | 'first' | 'last';

export const SELECT_NAVIGATION_KEYS: Readonly<Record<string, SelectDirection | undefined>> = {
  ArrowDown: 1, ArrowUp: -1, Home: 'first', End: 'last',
};

export const TYPEAHEAD_RESET_MS = 700;

/** One key searches after the active option; a longer query may keep it, as native selects do. */
export function typeaheadIndex(options: readonly SelectOption[], active: number, query: string): number {
  const prefix = normalizeSelectQuery(query);
  const start = query.length > 1 ? Math.max(active, 0) : active + 1;
  for (let count = 0; count < options.length; count++) {
    const index = (start + count) % options.length;
    if (!options[index].disabled && normalizeSelectQuery(options[index].label).startsWith(prefix)) return index;
  }
  return -1;
}

export interface TypeaheadKey {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
}

export interface Typeahead {
  /** Printable keys, with Space only while a query is being typed. */
  accepts(event: TypeaheadKey): boolean;
  find(options: readonly SelectOption[], active: number, key: string): number;
}

function isPrintableKey(event: TypeaheadKey): boolean {
  if (event.key.length !== 1) return false;
  return ![event.ctrlKey, event.metaKey, event.altKey].some(Boolean);
}

export function createTypeahead(now: () => number = Date.now): Typeahead {
  let query = '';
  let typedAt = -Infinity;
  const expired = () => now() - typedAt > TYPEAHEAD_RESET_MS;
  return {
    accepts(event) {
      if (!isPrintableKey(event)) return false;
      return event.key !== ' ' || !expired();
    },
    find(options, active, key) {
      query = (expired() ? '' : query) + key;
      typedAt = now();
      return typeaheadIndex(options, active, query);
    },
  };
}
