<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import { PickerPanel, Scroller, SearchInput, getI18n } from '@bagel/kit';

  const { t } = getI18n();

  let {
    id,
    value,
    zones,
    onPick
  }: {
    id: string;
    value: string;
    zones: string[];
    onPick: (zone: string) => void;
  } = $props();

  const MAX_RESULTS = 80;

  let open = $state(false);
  let query = $state('');
  let active = $state(0);
  let btnEl = $state<HTMLButtonElement>();
  let inputEl = $state<HTMLInputElement>();

  function norm(s: string): string {
    return s.trim().toLowerCase().normalize('NFD').replace(/\p{Diacritic}/gu, '').replace(/\s+/g, '_');
  }

  function city(zone: string): string {
    return zone.slice(zone.lastIndexOf('/') + 1).replace(/_/g, ' ');
  }

  function rank(zone: string, q: string): number {
    const z = zone.toLowerCase();
    if (z === q) return 0;
    const segs = z.split('/');
    if (segs[segs.length - 1].startsWith(q)) return 1;
    if (segs.some((s) => s.startsWith(q))) return 2;
    return z.includes(q) ? 3 : -1;
  }

  // An exact IANA name the browser list omits (UTC, US/Eastern) is still valid
  // to the bot, so a typed name the engine accepts is offered as-is.
  function exactZone(q: string): string {
    if (!q.includes('/') && q.length < 3) return '';
    try {
      return new Intl.DateTimeFormat('en-US', { timeZone: q.replace(/ /g, '_') }).resolvedOptions().timeZone;
    } catch {
      return '';
    }
  }

  const results = $derived.by(() => {
    const q = norm(query);
    if (!q) return zones.slice(0, MAX_RESULTS);
    const hits = zones
      .map((z) => ({ z, r: rank(z, q) }))
      .filter((h) => h.r >= 0)
      .sort((a, b) => a.r - b.r || a.z.localeCompare(b.z))
      .map((h) => h.z);
    const exact = exactZone(query.trim());
    if (exact && !hits.some((z) => z.toLowerCase() === exact.toLowerCase())) hits.unshift(exact);
    return hits.slice(0, MAX_RESULTS);
  });

  $effect(() => {
    void results;
    active = 0;
  });

  async function toggle() {
    open = !open;
    if (!open) return;
    query = '';
    await tick();
    inputEl?.focus();
  }

  function pick(zone: string) {
    open = false;
    btnEl?.focus();
    onPick(zone);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!results.length) return;
      const step = e.key === 'ArrowDown' ? 1 : -1;
      active = (active + step + results.length) % results.length;
      document.getElementById(`${id}-opt-${active}`)?.scrollIntoView({ block: 'nearest' });
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (results[active]) pick(results[active]);
    }
  }
</script>

<button
  {id}
  type="button"
  class="tz-trigger"
  class:unset={!value}
  aria-haspopup="dialog"
  aria-expanded={open}
  onclick={toggle}
  bind:this={btnEl}
>
  <span class="tz-value">{value || t('modules.tzUnset')}</span>
  <svg class="tz-caret" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"></path></svg>
</button>

<PickerPanel {open} anchor={btnEl} label={t('modules.tzPickerTitle')} width={320} maxHeight={380} onClose={() => (open = false)}>
  {#snippet children()}
    <SearchInput
      fill
      bind:value={query}
      bind:element={inputEl}
      placeholder={t('modules.tzSearchPh')}
      clearLabel={t('modules.tzSearchClear')}
      role="combobox"
      aria-expanded="true"
      aria-controls="{id}-list"
      aria-activedescendant={results.length ? `${id}-opt-${active}` : undefined}
      aria-autocomplete="list"
      autocomplete="off"
      spellcheck="false"
      onkeydown={onKey}
    />
    {#if results.length}
      <Scroller fill smooth>
        <ul class="tz-list" id="{id}-list" role="listbox" aria-label={t('modules.tzPickerTitle')}>
          {#each results as zone, i (zone)}
            <li
              id="{id}-opt-{i}"
              role="option"
              tabindex="-1"
              aria-selected={zone === value}
              class="tz-opt"
              class:active={i === active}
              onpointerenter={() => (active = i)}
              onclick={() => pick(zone)}
              onkeydown={onKey}
            >
              <span class="tz-city">{city(zone)}</span>
              <span class="tz-full">{zone}</span>
            </li>
          {/each}
        </ul>
      </Scroller>
    {:else}
      <p class="tz-empty">{t('modules.tzNoMatch')}</p>
    {/if}
  {/snippet}
</PickerPanel>

<style>
  .tz-trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    width: min(260px, 44vw);
    padding: 8px 12px;
    border: 1px solid var(--rule);
    border-radius: var(--bb-radius-sm);
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    text-align: left;
    cursor: pointer;
    transition: border-color var(--bb-dur-fast, 140ms) ease;
  }
  .tz-trigger:hover,
  .tz-trigger[aria-expanded='true'],
  .tz-trigger:focus-visible {
    outline: none;
    border-color: var(--bb-tan, #c9a87c);
  }
  .tz-trigger.unset .tz-value { color: var(--bb-muted); }
  .tz-value { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tz-caret {
    flex: none;
    width: 14px;
    height: 14px;
    fill: none;
    stroke: var(--bb-muted);
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  @media (max-width: 560px) {
    .tz-trigger { width: 100%; }
  }

  .tz-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .tz-opt {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 6px 10px;
    border-radius: var(--bb-radius-sm);
    cursor: pointer;
  }
  .tz-opt.active { background: var(--glass-fill-2); }
  .tz-opt[aria-selected='true'] .tz-city { color: var(--bb-green-glow, #52b788); }
  .tz-city { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-white); }
  .tz-full { font-family: var(--bb-font-mono); font-size: 10.5px; color: var(--bb-muted); }
  .tz-empty { margin: 0; padding: 6px 2px; font-family: var(--bb-font-body); font-size: 12px; font-style: italic; color: var(--bb-muted); }
</style>
