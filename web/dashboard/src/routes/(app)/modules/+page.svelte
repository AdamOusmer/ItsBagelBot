<script lang="ts">
  import { Kbd } from '@bagel/kit';
  import { SearchInput } from '@bagel/kit';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount, untrack } from 'svelte';
  import { reconcileModuleToggles } from '$lib/module-toggle';
  import { page } from '$app/state';
  import { replaceState } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    PageHead,
    AlertBanner,
    Card,
    Heading,
    Text,
    EmptyState,
    toast,
    getI18n,
    filterModuleIndex,
    groupModulesByCategory,
    readModuleIndexQuery,
    writeModuleIndexQuery,
    MODULE_CATEGORY_I18N,
    SectionNav,
    categoryAnchorId,
    categoryHref,
    moduleHref,
    tModuleLabel,
    tModuleTagline,
    tModuleDescription,
    type ModuleState
  } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import ModuleIndexRow from '$lib/components/modules/ModuleIndexRow.svelte';

  let { data } = $props();

  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let items = $state<ModuleState[]>(data.modules ?? []);
  // svelte-ignore state_referenced_locally
  let seed = data.modules;
  const pendingToggles = new Map<string, boolean>();
  $effect(() => {
    if (data.modules !== seed) {
      seed = data.modules;
      items = reconcileModuleToggles(data.modules ?? [], untrack(() => items), pendingToggles);
    }
  });

  // svelte-ignore state_referenced_locally
  const initial = readModuleIndexQuery(
    page.url.searchParams,
    [...new Set((data.modules ?? []).map((m) => m.def.category))]
  );

  let searchQuery = $state(initial.q);
  let statusFilter = $state<'all' | 'on'>(initial.status === 'on' ? 'on' : 'all');
  const STATUS_OPTIONS = ['all', 'on'] as const;
  const seededOn = $derived(new Set((data.modules ?? []).filter((m) => m.enabled).map((m) => m.def.id)));

  const activeCount = $derived(items.filter((m) => m.enabled).length);
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
  function catHint(name: string): string {
    const keys = MODULE_CATEGORY_I18N[name];
    return keys ? t(keys.hint) : '';
  }

  const navItems = $derived(
    groups.map((group) => ({
      href: categoryHref(group.name),
      label: catLabel(group.name),
      count: group.modules.length
    }))
  );

  // Use only modules present in this viewer's catalog so shortcuts respect
  // the same delegation rules as the rows below.
  const shortcuts = $derived(
    ['songqueue', 'timers', 'loyalty', 'quotes'].flatMap((id) => {
      const module = items.find((m) => m.def.id === id);
      return module ? [module] : [];
    })
  );

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
    const url = new URL(page.url);
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
        const payload =
          result.type === 'success' || result.type === 'failure'
            ? (result.data as { ok?: boolean; revision?: number } | undefined)
            : undefined;
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
          toast('err', t('modules.couldNotToggle', { label: tModuleLabel(t, m.def) }));
        }
      };
    };

  let searchInput = $state<HTMLInputElement | undefined>(undefined);
  function isTyping(e: KeyboardEvent): boolean {
    const el = e.target as HTMLElement | null;
    return !!el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable);
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && document.activeElement === searchInput && searchQuery) {
      e.preventDefault();
      searchQuery = '';
      return;
    }
    if (isTyping(e) || e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === '/') {
      e.preventDefault();
      searchInput?.focus();
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<section class="screen active">
  <PageHead
    eyebrow={t('modules.eyebrow')}
    description={t('modules.description', { active: activeCount, total: items.length })}
  >{t('modules.titlePre')}<em>{t('modules.titleEm')}</em></PageHead>

  {#if data.degraded}
    <AlertBanner>{t('modules.degraded')}</AlertBanner>
  {/if}

  <div class="deck">
    <label class="find-label" for="module-search">{t('modules.searchLabel')}</label>
    <div class="find-row">
    <div class="find">
      <SearchInput id="module-search" bind:value={searchQuery} bind:element={searchInput} placeholder={t('modules.searchPlaceholder')}
        aria-label={t('modules.searchLabel')} aria-describedby="module-search-hint" clearLabel={t('modules.searchClear')} autocomplete="off" enterkeyhint="search" fill />
      {#if !searchQuery}<span class="keys" aria-hidden="true"><Kbd>/</Kbd></span>{/if}
    </div>
    <div class="bb-tabs status-filter" role="radiogroup" aria-label={t('modules.statusFilterLabel')}>
      {#each STATUS_OPTIONS as opt (opt)}
        <button
          type="button"
          class="bb-tab {statusFilter === opt ? 'is-active' : ''}"
          role="radio"
          aria-checked={statusFilter === opt}
          onclick={() => (statusFilter = opt)}
        >{opt === 'all' ? t('modules.filterAll') : t('modules.filterOn')}</button>
      {/each}
    </div>
    </div>
  </div>

  <div class="find-help">
    <p id="module-search-hint">{t('modules.openHint')}</p>
    <p class="result-count" aria-live="polite">{t('modules.resultCount', { shown: filtered.length, total: items.length })}</p>
  </div>

  {#if shortcuts.length}
    <nav class="shortcuts" class:concealed={!!searchQuery.trim()} aria-label={t('modules.quickAccess')}>
      <span class="shortcut-label">{t('modules.quickAccess')}</span>
      {#each shortcuts as module (module.def.id)}
        <a class="bb-btn bb-btn--ghost" href={moduleHref(module.def)}>
          {#if module.def.id === 'songqueue'}<Icon name="music" size={16} />{/if}
          {tModuleLabel(t, module.def)}
          <Icon name="chevron" size={12} class="shortcut-chevron" />
        </a>
      {/each}
    </nav>
  {/if}

  {#if groups.length === 0}
    <EmptyState title={t('modules.noMatch')} body={t('modules.noMatchBody')}>
      <button type="button" class="bb-btn bb-btn--ghost" onclick={clearSearch}>{t('modules.searchClear')}</button>
    </EmptyState>
  {:else}
    <div class="index">
      <SectionNav label={t('modules.catNav')} items={navItems} />
      <div class="families">
        {#each groups as group (group.name)}
          <section
            class="family"
            id={categoryAnchorId(group.name)}
            tabindex="-1"
            aria-labelledby="family-{categoryAnchorId(group.name)}"
          >
            <header class="family-head">
              <Heading level={2} class="family-title" id="family-{categoryAnchorId(group.name)}"
                >{catLabel(group.name)}</Heading
              >
              {#if catHint(group.name)}
                <Text size="sm" tone="muted" class="family-hint">{catHint(group.name)}</Text>
              {/if}
            </header>
            <Card style="padding:0">
              {#each group.modules as m (m.def.id)}
                <ModuleIndexRow module={m} status={modStatus[m.def.id] ?? 'idle'} toggleSubmit={toggleSubmit(m)} />
              {/each}
            </Card>
          </section>
        {/each}
      </div>
    </div>
  {/if}
</section>

<style>
  .find-label {
    display: block;
    margin-bottom: 8px;
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
  }
  .find-help {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 6px 20px;
    margin-bottom: 16px;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
  }
  .find-help p { margin: 0; }
  .result-count { flex: none; }
  .shortcuts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 22px; }
  .shortcut-label { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 12px; margin-right: 4px; }
  .shortcuts :global(.shortcut-chevron) { transform: rotate(-90deg); }
  .shortcuts.concealed { visibility: hidden; }
  .deck {
    position: sticky;
    top: calc(58px + env(safe-area-inset-top, 0px));
    z-index: 5;
    padding: 10px 0 14px;
    margin: 0 0 10px;
    background: var(--bb-bg-0);
    border-bottom: 1px solid var(--rule);
  }
  .find { min-width: 0; position: relative; }
  .find-row { display: flex; align-items: center; gap: 12px; }
  .find-row .find { flex: 1; }
  .status-filter { flex: none; }
  .find .keys { position: absolute; right: 12px; top: 50%; transform: translateY(-50%); pointer-events: none; }
  .keys {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    letter-spacing: 0.04em;
    white-space: nowrap;
  }

  .index {
    display: grid;
    gap: 18px 32px;
    --bb-tabs-sticky-top: calc(58px + env(safe-area-inset-top, 0px) + 98px);
  }
  @media (min-width: 761px) {
    .index {
      grid-template-columns: 10rem minmax(0, 1fr);
    }
  }
  .families { display: flex; flex-direction: column; gap: 28px; min-width: 0; }
  .family {
    scroll-margin-top: calc(58px + env(safe-area-inset-top, 0px) + 102px);
  }
  .family:focus { outline: none; }
  .family:target :global(.family-title) { color: var(--bb-tan-pale, var(--bb-tan-light)); }
  .family-head { margin-bottom: 10px; }
  :global(.family-title) { font-size: 1.15rem; letter-spacing: -0.02em; }
  :global(.family-hint) { margin-top: 4px; max-width: 52ch; }
  @media (max-width: 760px) {
    .deck {
      top: calc(52px + env(safe-area-inset-top, 0px));
    }
    .index {
      --bb-tabs-sticky-top: calc(52px + env(safe-area-inset-top, 0px) + 98px);
    }
    .family {
      scroll-margin-top: calc(52px + env(safe-area-inset-top, 0px) + 102px);
    }
    .keys { display: none; }
  }
</style>
