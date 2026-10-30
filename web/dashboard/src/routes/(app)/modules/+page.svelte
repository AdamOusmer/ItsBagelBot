<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount, untrack } from 'svelte';
  import { isShortcut } from '@bagel/ui/lib/hotkeys';
  import { reconcileModuleToggles } from '#lib/module-toggle';
  import { hydrateModules } from '#lib/module-states';
  import { reseedOnChange } from '#lib/client-actions.svelte';
  import { page } from '$app/state';
  import { replaceState } from '$app/navigation';
  import type { SubmitFunction } from '$app/forms';
  import Kbd from '@bagel/ui/svelte/Kbd.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import {
    getI18n,
    filterModuleIndex,
    groupModulesByCategory,
    readModuleIndexQuery,
    writeModuleIndexQuery,
    MODULE_CATEGORY_I18N,
    categoryAnchorId,
    tModuleLabel,
    tModuleTagline,
    tModuleDescription,
    actionPayload,
    type ActionOk,
    type ModuleState
  } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import ModuleIndexRow from '#lib/components/modules/ModuleIndexRow.svelte';

  let { data } = $props();

  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let items = $state<ModuleState[]>(hydrateModules(data.modules ?? []));
  const pendingToggles = new Map<string, boolean>();
  reseedOnChange(() => data.modules, (next) => {
    items = reconcileModuleToggles(hydrateModules(next ?? []), untrack(() => items), pendingToggles);
  });

  // svelte-ignore state_referenced_locally
  const initial = readModuleIndexQuery(
    new URLSearchParams(page.url.search),
    [...new Set(items.map((m) => m.def.category))]
  );

  let searchQuery = $state(initial.q);
  let statusFilter = $state<'all' | 'on'>(initial.status === 'on' ? 'on' : 'all');
  const statusOptions = $derived([
    { value: 'all', label: t('modules.filterAll') },
    { value: 'on', label: t('modules.filterOn') }
  ]);
  const seededOn = $derived(new Set((data.modules ?? []).filter((m) => m.enabled).map((m) => m.id)));

  const filtered = $derived(
    filterModuleIndex(items, { q: searchQuery, category: '', status: 'all' }, (def) =>
      [tModuleLabel(t, def), tModuleTagline(t, def), tModuleDescription(t, def)].join('\n')
    ).filter((m) => statusFilter === 'all' || seededOn.has(m.def.id))
  );
  const groups = $derived(groupModulesByCategory(filtered));

  function catLabel(name: string): string {
    const keys = MODULE_CATEGORY_I18N[name];
    return keys ? t(keys.label) : name;
  }

  const groupActive = (modules: ModuleState[]): number => modules.filter((m) => m.enabled).length;

  function clearSearch() {
    searchQuery = '';
    statusFilter = 'all';
  }

  let urlReady = $state(false);
  onMount(() => {
    urlReady = true;
  });
  $effect(() => {
    if (!urlReady) return;
    const url = new URL(page.url.href);
    writeModuleIndexQuery(url, { q: searchQuery, category: '', status: statusFilter });
    const next = url.pathname + url.search;
    if (next !== page.url.pathname + page.url.search) replaceState(url, {});
  });

  let modStatus = $state<Record<string, SaveState>>({});
  const timers = new Map<string, ReturnType<typeof setTimeout>[]>();
  function setStatus(id: string, s: SaveState) {
    for (const tm of timers.get(id) ?? []) clearTimeout(tm);
    timers.delete(id);
    modStatus = { ...modStatus, [id]: s };
  }
  function ackSaved(id: string) {
    setStatus(id, 'saved');
    timers.set(id, [
      setTimeout(() => (modStatus = { ...modStatus, [id]: 'live' }), 2500),
      setTimeout(() => (modStatus = { ...modStatus, [id]: 'idle' }), 7000)
    ]);
  }

  const toggleSubmit =
    (m: ModuleState): SubmitFunction =>
    () => {
      const was = m.enabled;
      pendingToggles.set(m.def.id, !was);
      items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: !was } : x));
      setStatus(m.def.id, 'saving');
      return async ({ result }) => {
        const payload = actionPayload<ActionOk & { revision?: number }>(result);
        pendingToggles.delete(m.def.id);
        if (result.type === 'success' && payload?.ok) {
          items = items.map((x) => x.def.id === m.def.id
            ? { ...x, enabled: !was, revision: payload.revision ?? x.revision }
            : x);
          ackSaved(m.def.id);
        } else {
          items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: was } : x));
          setStatus(m.def.id, 'error');
          timers.set(m.def.id, [setTimeout(() => (modStatus = { ...modStatus, [m.def.id]: 'idle' }), 4000)]);
          toast('danger', t('modules.couldNotToggle', { label: tModuleLabel(t, m.def) }));
        }
      };
    };

  let searchInput = $state<HTMLInputElement | undefined>(undefined);
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && document.activeElement === searchInput && searchQuery) {
      e.preventDefault();
      searchQuery = '';
      return;
    }
    if (!isShortcut(e)) return;
    if (e.key === '/') {
      e.preventDefault();
      searchInput?.focus();
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<section class="screen active mods">
  <header class="mods__hero">
    <span class="mods__eyebrow"><span class="mods__beacon" aria-hidden="true"></span>{t('modules.eyebrow')}</span>
    <h1 class="mods__title"><span class="mods__word">{t('modules.titlePre')}</span><span class="mods__word">{t('modules.titleEm')}</span></h1>
  </header>

  {#if data.degraded}
    <AlertBanner>{t('modules.degraded')}</AlertBanner>
  {/if}

  <div class="mods__toolbar">
    <div class="mods__find">
      <SearchInput id="module-search" bind:value={searchQuery} bind:element={searchInput} placeholder={t('modules.searchPlaceholder')}
        aria-label={t('modules.searchLabel')} clearLabel={t('modules.searchClear')} autocomplete="off" enterkeyhint="search" fill />
      {#if !searchQuery}<span class="mods__keys" aria-hidden="true"><Kbd>/</Kbd></span>{/if}
    </div>
    <SegmentedControl
      options={statusOptions}
      label={t('modules.statusFilterLabel')}
      bind:value={() => statusFilter, (v) => (statusFilter = v === 'on' ? 'on' : 'all')}
    />
  </div>

  <div class="mods__help">
    <span aria-live="polite">{t('modules.resultCount', { shown: filtered.length, total: items.length })}</span>
  </div>

  {#if groups.length === 0}
    <EmptyState title={t('modules.noMatch')} body={t('modules.noMatchBody')}>
      <Button variant="ghost" onclick={clearSearch}>{t('modules.searchClear')}</Button>
    </EmptyState>
  {:else}
    <div class="mods__groups">
      {#each groups as group (group.name)}
        <section
          class="mods__group"
          id={categoryAnchorId(group.name)}
          tabindex="-1"
          aria-labelledby="family-{categoryAnchorId(group.name)}"
        >
          <Card class="mods__lane">
            <header class="mods__lane-head">
              <h2 class="mods__lane-title" id="family-{categoryAnchorId(group.name)}">{catLabel(group.name)}</h2>
              <span class="mods__lane-count">{groupActive(group.modules)} / {group.modules.length} {t('modules.statusOn')}</span>
            </header>
            <div class="mods__rows">
              {#each group.modules as m (m.def.id)}
                <ModuleIndexRow module={m} status={modStatus[m.def.id] ?? 'idle'} toggleSubmit={toggleSubmit(m)} />
              {/each}
            </div>
          </Card>
        </section>
      {/each}
    </div>
  {/if}
</section>

<style>
  .mods {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    padding-bottom: var(--bb-space-section);
  }

  .mods__hero {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-3);
    padding-top: var(--bb-space-4);
  }

  .mods__eyebrow {
    display: inline-flex;
    align-items: center;
    gap: var(--bb-space-2);
    font-size: var(--bb-text-md);
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--bb-green-glow);
    text-shadow: 0 0 18px rgba(var(--bb-green-glow-rgb), 0.45);
  }

  .mods__beacon {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 10px var(--bb-green-glow);
  }

  .mods__title {
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(2.25rem, 4.4vw, 3.5rem);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.06;
  }

  .mods__word {
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
  }

  .mods__toolbar {
    position: sticky;
    top: calc(var(--bb-topbar-height) + 4px);
    z-index: 5;
    display: flex;
    align-items: center;
    gap: var(--bb-space-3);
    padding: var(--bb-space-2);
    border-radius: var(--bb-radius-md);
    background: var(--bb-elev-1);
    box-shadow: var(--bb-depth-2);
  }

  .mods__find {
    position: relative;
    flex: 1;
    min-width: 0;
  }

  .mods__keys {
    position: absolute;
    top: 50%;
    right: 12px;
    transform: translateY(-50%);
    pointer-events: none;
  }

  .mods__help {
    display: flex;
    justify-content: flex-end;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
  }

  .mods__groups {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
  }

  .mods__group {
    scroll-margin-top: calc(var(--bb-topbar-height) + 84px);
  }

  .mods__group:focus { outline: none; }

  .mods :global(.mods__lane) {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-3);
    padding: var(--bb-space-5) var(--bb-space-5) var(--bb-space-3);
  }

  .mods__lane-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--bb-space-3);
  }

  .mods__lane-title {
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: 1.5rem;
    font-weight: 500;
    letter-spacing: -0.03em;
    color: var(--bb-paper);
  }

  .mods__group:target .mods__lane-title { color: var(--bb-tan-pale); }

  .mods__lane-count {
    flex: none;
    font: var(--bb-text-xs)/1.6 var(--bb-font-mono);
    color: var(--bb-muted);
  }

  .mods__rows {
    display: flex;
    flex-direction: column;
    margin-inline: calc(var(--bb-space-3) * -1);
  }

  .mods__rows > :global(* + *) {
    box-shadow: inset 0 1px 0 rgba(var(--bb-white-rgb), 0.05);
  }

  @media (max-width: 760px) {
    .mods__keys { display: none; }
    .mods :global(.mods__lane) { padding-inline: var(--bb-space-4); }
  }
</style>
