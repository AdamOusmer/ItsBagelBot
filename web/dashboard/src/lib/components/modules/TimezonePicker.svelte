<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // Domain-specific ranking and accepted IANA aliases; presentation lives in ui.
  import { Select, getI18n } from '@bagel/kit';
  import type { SelectOption } from '@bagel/ui/lib/select';

  const { t } = getI18n();
  let { id, value, zones, disabled = false, onPick }: {
    id: string;
    value: string;
    zones: string[];
    disabled?: boolean;
    onPick: (zone: string) => void;
  } = $props();

  function option(zone: string): SelectOption {
    return {
      value: zone,
      label: zone.slice(zone.lastIndexOf('/') + 1).replace(/_/g, ' '),
      description: zone,
      triggerLabel: zone
    };
  }
  const options = $derived(zones.map(option));
  function norm(s: string): string {
    return s.trim().toLowerCase().normalize('NFD').replace(/\p{Diacritic}/gu, '').replace(/\s+/g, '_');
  }
  function rank(zone: string, query: string): number {
    const lower = zone.toLowerCase();
    if (lower === query) return 0;
    const segments = lower.split('/');
    if (segments[segments.length - 1].startsWith(query)) return 1;
    if (segments.some((segment) => segment.startsWith(query))) return 2;
    return lower.includes(query) ? 3 : -1;
  }
  function filterOptions(items: readonly SelectOption[], query: string): SelectOption[] {
    const q = norm(query);
    if (!q) return items.slice(0, 80);
    const hits = items.map((item) => ({ item, rank: rank(item.value, q) }))
      .filter((hit) => hit.rank >= 0)
      .sort((a, b) => a.rank - b.rank || a.item.value.localeCompare(b.item.value))
      .map((hit) => hit.item);
    // IANA aliases omitted by the browser's zone inventory remain selectable.
    if (query.includes('/') || query.trim().length >= 3) {
      try {
        const exact = new Intl.DateTimeFormat('en-US', { timeZone: query.trim().replace(/ /g, '_') }).resolvedOptions().timeZone;
        if (!hits.some((item) => item.value.toLowerCase() === exact.toLowerCase())) hits.unshift(option(exact));
      } catch { /* A partial city search is not necessarily an IANA identifier. */ }
    }
    return hits.slice(0, 80);
  }
</script>

<div class="timezone-picker">
  <Select {id} {value} {disabled} {options} {filterOptions} searchable fill
    label={t('modules.tzPickerTitle')} placeholder={t('modules.tzUnset')}
    searchPlaceholder={t('modules.tzSearchPh')} searchClearLabel={t('modules.tzSearchClear')} emptyLabel={t('modules.tzNoMatch')}
    onchange={(event) => onPick(event.currentTarget.value)} />
</div>

<style>
  .timezone-picker { width: min(260px, 44vw); }
  @media (max-width: 560px) { .timezone-picker { width: 100%; } }
</style>
