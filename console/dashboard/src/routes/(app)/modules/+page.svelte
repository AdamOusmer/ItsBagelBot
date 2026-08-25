<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The whole catalog is on the page. A Chat/Community/Games menu hid Song
  // Requests behind a folder nobody can guess; streamers had to know the
  // taxonomy before they could see the list. Search is the only filter.
  // Category jumps are shared SectionNav hash links (not a desktop-only
  // scrollspy). Enabled rows sort to the top of their group so "what is on"
  // does not need a second place to click.
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { replaceState } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    PageHead,
    AlertBanner,
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
    type ModuleState
  } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import ModuleIndexRow from '$lib/components/modules/ModuleIndexRow.svelte';

  let { data } = $props();

  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let items = $state<ModuleState[]>(data.modules ?? []);
  // svelte-ignore state_referenced_locally
  let seed = data.modules;
  $effect(() => {
    if (data.modules !== seed) {
      seed = data.modules;
      items = data.modules ?? [];
    }
  });

  // svelte-ignore state_referenced_locally
  const initial = readModuleIndexQuery(
    page.url.searchParams,
    [...new Set((data.modules ?? []).map((m) => m.def.category))]
  );

  let searchQuery = $state(initial.q);

  const activeCount = $derived(items.filter((m) => m.enabled).length);
  const filtered = $derived(filterModuleIndex(items, { q: searchQuery, category: '', status: 'all' }));
  const groups = $derived(
    groupModulesByCategory(filtered).map((group) => ({
      ...group,
      modules: [
        ...group.modules.filter((m) => m.enabled),
        ...group.modules.filter((m) => !m.enabled)
      ]
    }))
  );

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

  function clearSearch() {
    searchQuery = '';
  }

  let urlReady = $state(false);
  onMount(() => {
    urlReady = true;
  });
  $effect(() => {
    if (!urlReady) return;
    const url = new URL(page.url);
    // Drop cat/status left over from the folder-menu layout so a shared
    // ?cat=community link cannot hide the rest of the catalog again.
    writeModuleIndexQuery(url, { q: searchQuery, category: '', status: 'all' });
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
      items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: !was } : x));
      setStatus(m.def.id, 'saving');
      return async ({ result }) => {
        const payload =
          result.type === 'success' || result.type === 'failure'
            ? (result.data as { ok?: boolean } | undefined)
            : undefined;
        if (result.type === 'success' && payload?.ok) {
          ackSaved(m.def.id);
        } else {
          items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: was } : x));
          setStatus(m.def.id, 'error');
          timers.set(m.def.id, [setTimeout(() => (modStatus = { ...modStatus, [m.def.id]: 'idle' }), 4000)]);
          toast('err', t('modules.couldNotToggle', { label: m.def.label }));
        }
      };
    };

  let searchInput = $state<HTMLInputElement | null>(null);
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
    <label class="search find">
      <Icon name="search" size={15} />
      <span class="sr-only">{t('modules.searchLabel')}</span>
      <input
        type="search"
        bind:value={searchQuery}
        bind:this={searchInput}
        placeholder={t('modules.searchPlaceholder')}
        autocomplete="off"
        enterkeyhint="search"
      />
      {#if searchQuery}
        <button type="button" class="clear" aria-label={t('modules.searchClear')} onclick={clearSearch}>
          <Icon name="x" size={12} />
        </button>
      {:else}
        <span class="keys" aria-hidden="true"><kbd class="hint">/</kbd></span>
      {/if}
    </label>
  </div>

  <p class="sr-only" aria-live="polite">{t('modules.resultCount', { shown: filtered.length, total: items.length })}</p>

  {#if groups.length === 0}
    <EmptyState icon="search" title={t('modules.noMatch')} body={t('modules.noMatchBody')}>
      <button type="button" class="btn" onclick={clearSearch}>{t('modules.searchClear')}</button>
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
              <h2 id="family-{categoryAnchorId(group.name)}">{catLabel(group.name)}</h2>
              {#if catHint(group.name)}
                <p>{catHint(group.name)}</p>
              {/if}
            </header>
            <div class="family-list">
              {#each group.modules as m (m.def.id)}
                <ModuleIndexRow module={m} status={modStatus[m.def.id] ?? 'idle'} toggleSubmit={toggleSubmit(m)} />
              {/each}
            </div>
          </section>
        {/each}
      </div>
    </div>
  {/if}
</section>

<style>
  .deck {
    position: sticky;
    top: calc(58px + env(safe-area-inset-top, 0px));
    z-index: 5;
    padding: 10px 0 14px;
    margin: 0 0 22px;
    background: var(--bb-bg-0);
    border-bottom: 1px solid var(--rule);
  }
  .find { width: 100%; min-width: 0; }
  .find input[type="search"]::-webkit-search-cancel-button { display: none; }
  .keys {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    letter-spacing: 0.04em;
    white-space: nowrap;
  }
  .clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    flex: none;
    border: none;
    background: transparent;
    color: var(--bb-muted);
    cursor: pointer;
    border-radius: 8px;
  }
  .clear:hover { color: var(--bb-white); }

  .index {
    display: grid;
    gap: 18px 32px;
    --section-nav-sticky-top: calc(58px + env(safe-area-inset-top, 0px) + 68px);
  }
  /* One column on a phone (chips above the list). Two columns when there is
     room for a ~10rem rail — reflow, not display:none. The old sidebar hid
     itself below 980px so only a wide desktop could jump. */
  @media (min-width: 761px) {
    .index {
      grid-template-columns: 10rem minmax(0, 1fr);
    }
  }
  .families { display: flex; flex-direction: column; gap: 28px; min-width: 0; }
  .family {
    /* Hash + programmatic focus land below the sticky topbar and search. */
    scroll-margin-top: calc(58px + env(safe-area-inset-top, 0px) + 72px);
  }
  .family:focus { outline: none; }
  .family:target .family-head h2 { color: var(--bb-tan-pale, var(--bb-tan-light)); }
  .family-head { margin-bottom: 10px; }
  .family-head h2 {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 1.15rem;
    letter-spacing: -0.02em;
    color: var(--bb-white);
    margin: 0;
  }
  .family-head p {
    margin: 4px 0 0;
    font-size: 13px;
    line-height: 1.45;
    color: var(--bb-muted);
    max-width: 52ch;
  }
  .family-list {
    border: 1px solid var(--rule);
    border-radius: 8px;
    background: rgba(240, 236, 228, 0.018);
    overflow: hidden;
  }

  @media (max-width: 760px) {
    .deck {
      top: calc(52px + env(safe-area-inset-top, 0px));
    }
    .index {
      --section-nav-sticky-top: calc(52px + env(safe-area-inset-top, 0px) + 68px);
    }
    .family {
      scroll-margin-top: calc(52px + env(safe-area-inset-top, 0px) + 72px);
    }
    .keys { display: none; }
  }
</style>
